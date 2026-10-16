// SPDX-License-Identifier: MIT
package native

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/rlimit"
	"nfsmon/internal/collect"
	"nfsmon/internal/layout"
)

type Source struct {
	objs   collectorObjects
	links  []link.Link
	layout layout.Layout
	names  map[nameKey]string
}
type nameKey struct {
	Ino      uint64
	Dev, Pad uint32
}

func Open(l layout.Layout, proto uint32, group string) (_ *Source, err error) {
	s := &Source{layout: l, names: make(map[nameKey]string)}
	defer func() {
		if err != nil {
			s.Close()
		}
	}()
	if err = rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("memlock: %w", err)
	}
	if err = loadCollectorObjects(&s.objs, nil); err != nil {
		return nil, fmt.Errorf("load BPF: %w", err)
	}
	o := l.Offsets
	cfg := collectorConfigT{Transport: proto, Names: 1,
		RqAddr: o.RqAddr, RqDaddr: o.RqDaddr, RqVers: o.RqVers, RqProt: o.RqProt, RqUid: o.RqUID, RqGid: o.RqGID,
		FileInode: o.FileInode, FileDentry: o.FileDentry, InodeIno: o.InodeIno, InodeSb: o.InodeSB, SbDev: o.SBDev, DentryParent: o.DentryParent, DentryName: o.DentryName}
	for i, h := range l.Hooks {
		cfg.Hooks[i].RqArg = h.RqArg
		cfg.Hooks[i].FileArg = h.FileArg
		cfg.Hooks[i].CountArg = h.CountArg
		cfg.Hooks[i].CountBytes = h.CountBytes
		cfg.Hooks[i].FileWrapper = h.FileWrapper
	}
	if err = s.objs.Config.Update(uint32(0), cfg, ebpf.UpdateAny); err != nil {
		return nil, err
	}
	pairs := [][2]*ebpf.Program{{s.objs.ReadStart, s.objs.ReadEnd}, {s.objs.SpliceStart, s.objs.SpliceEnd}, {s.objs.WriteStart, s.objs.WriteEnd}}
	for i, h := range l.Hooks {
		// Return first, so an entry cannot create an inflight record without a return hook.
		var ret, entry link.Link
		ret, err = link.Kretprobe(h.Symbol, pairs[i][1], &link.KprobeOptions{RetprobeMaxActive: 4096, TraceFSPrefix: "nfsmon"})
		if err != nil {
			return nil, fmt.Errorf("kretprobe %s: %w", h.Symbol, err)
		}
		s.links = append(s.links, ret)
		entry, err = link.Kprobe(h.Symbol, pairs[i][0], &link.KprobeOptions{TraceFSPrefix: "nfsmon"})
		if err != nil {
			return nil, fmt.Errorf("kprobe %s: %w", h.Symbol, err)
		}
		s.links = append(s.links, entry)
	}
	return s, nil
}
func (s *Source) Description() string { return "native eBPF / " + s.layout.Source }
func (s *Source) Close() error {
	for i := len(s.links) - 1; i >= 0; i-- {
		_ = s.links[i].Close()
	}
	s.links = nil
	return s.objs.Close()
}
func decode(b [16]byte, f uint32) netip.Addr {
	if f == 2 {
		return netip.AddrFrom4([4]byte{b[0], b[1], b[2], b[3]})
	}
	return netip.AddrFrom16(b).Unmap()
}
func cstr(b []uint8) string { return strings.SplitN(string(b), "\x00", 2)[0] }
func (s *Source) Read(ctx context.Context) (collect.Snapshot, error) {
	snap := collect.Snapshot{At: time.Now()}
	if err := ctx.Err(); err != nil {
		return snap, err
	}
	{
		it := s.objs.Names.Iterate()
		var k nameKey
		var v collectorNameT
		for it.Next(&k, &v) {
			s.names[k] = cstr(v.Parent[:]) + "/" + cstr(v.Name[:])
		}
		if err := it.Err(); err != nil {
			return snap, err
		}
	}
	var err error
	snap.Records, err = s.readRecords(s.objs.Counts)
	if err != nil {
		return snap, fmt.Errorf("detail map: %w", err)
	}
	snap.Clients, err = s.readRecords(s.objs.Clients)
	if err != nil {
		return snap, fmt.Errorf("client map: %w", err)
	}
	dit := s.objs.Diagnostics.Iterate()
	var dk collectorDiagnosticKey
	var dv collectorDiagnosticT
	diagnostics := make(map[collectorDiagnosticKey]*collect.Diagnostic)
	for dit.Next(&dk, &dv) {
		var peaks []uint64
		if err := s.objs.DiagnosticMax.Lookup(dk, &peaks); err != nil && !errors.Is(err, ebpf.ErrKeyNotExist) {
			return snap, fmt.Errorf("latency maximum map: %w", err)
		}
		op := diagnosticOperation(dv, peaks)
		key := dk
		key.Write = 0
		d := diagnostics[key]
		if d == nil {
			d = &collect.Diagnostic{Client: decode(dk.Client, dk.Family), Server: decode(dk.Server, dk.Family), Transport: dk.Transport}
			diagnostics[key] = d
		}
		if dk.Write != 0 {
			d.Write = op
		} else {
			d.Read = op
		}
	}
	if err := dit.Err(); err != nil {
		return snap, fmt.Errorf("latency map: %w", err)
	}
	for _, d := range diagnostics {
		snap.Diagnostics = append(snap.Diagnostics, *d)
	}
	var health []collectorHealthT
	if err := s.objs.Health.Lookup(uint32(0), &health); err != nil {
		return snap, err
	}
	var bad, file, client, flight, overwritten, failed, names, diagnostic uint64
	for _, h := range health {
		bad += h.BadReads
		file += h.FileFull
		client += h.ClientFull
		flight += h.InflightFull
		overwritten += h.Overwritten
		failed += h.FailedOps
		names += h.NameDrops
		diagnostic += h.DiagnosticDrops
	}
	if bad+file+client+flight+overwritten+names+failed+diagnostic > 0 {
		snap.Health = fmt.Sprintf("read_errors=%d file_drops=%d client_drops=%d inflight_drops=%d missed_or_overwritten=%d failed_ops=%d name_hint_drops=%d diagnostic_drops=%d", bad, file, client, flight, overwritten, failed, names, diagnostic)
	}
	snap.CheckDiagnosticHealth()
	return snap, nil
}

func diagnosticOperation(value collectorDiagnosticT, peaks []uint64) collect.OpDiagnostics {
	op := collect.OpDiagnostics{Calls: value.Calls, Errors: value.Errors, TotalNS: value.TotalNs, Buckets: value.Buckets}
	for _, peak := range peaks {
		op.MaxNS = max(op.MaxNS, peak)
	}
	return op
}

func (s *Source) readRecords(m *ebpf.Map) ([]collect.Record, error) {
	it := m.Iterate()
	var k collectorKeyT
	var values []collectorValT
	var records []collect.Record
	for it.Next(&k, &values) {
		records = append(records, recordFromKey(k, values, s.names[nameKey{k.Ino, k.Dev, 0}]))
	}
	if err := it.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func recordFromKey(k collectorKeyT, values []collectorValT, name string) collect.Record {
	var total collect.Counters
	for _, v := range values {
		total = total.Add(collect.Counters{Read: v.R_bytes, Write: v.W_bytes, ReadOps: v.R_ops, WriteOps: v.W_ops})
	}
	return collect.Record{Client: decode(k.Client, k.Family), Server: decode(k.Server, k.Family), Transport: k.Transport, UID: k.Uid, GID: k.Gid, Device: k.Dev, Inode: k.Ino, Name: name, Counters: total}
}

// SPDX-License-Identifier: MIT
package app

import (
	"context"
	"fmt"
	"math"
	"net/netip"
	"time"

	"nfsmon/internal/collect"
)

// demoSource exercises the real history and UI without opening any kernel probes.
type demoSource struct {
	start, last time.Time
	records     []collect.Record
	diagnostics []collect.Diagnostic
}

func newDemoSource() *demoSource {
	now := time.Now()
	s := &demoSource{start: now, last: now}
	for i := 0; i < 8; i++ {
		transport := []uint32{256, 6, 256, 6, 6, 17, 256, 6}[i]
		s.records = append(s.records, collect.Record{
			Client: netip.AddrFrom4([4]byte{192, 0, 2, byte(21 + i)}),
			Server: netip.MustParseAddr("192.0.2.10"),
			UID:    uint32(1000 + i%3), GID: 1000, Device: 2049,
			Inode: uint64(120000 + i), Name: fmt.Sprintf("projects/dataset-%02d.bin", i+1),
			Transport: transport,
		})
		r := s.records[len(s.records)-1]
		s.diagnostics = append(s.diagnostics, collect.Diagnostic{Client: r.Client, Server: r.Server, Transport: transport})
	}
	return s
}

func (s *demoSource) Read(ctx context.Context) (collect.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return collect.Snapshot{}, err
	}
	return s.sample(time.Now()), nil
}

func (s *demoSource) sample(at time.Time) collect.Snapshot {
	dt := max(0, at.Sub(s.last).Seconds())
	t := at.Sub(s.start).Seconds()
	for i := range s.records {
		wave := 0.65 + 0.35*math.Sin(t/9+float64(i))
		r := (150.0 / float64(i+1)) * wave * (1 << 20)
		w := (65.0 / float64(i+1)) * (1.3 - wave) * (1 << 20)
		c := &s.records[i].Counters
		delta := collect.Counters{Read: uint64(r * dt), Write: uint64(w * dt), ReadOps: uint64(r * dt / 65536), WriteOps: uint64(w * dt / 65536)}
		*c = c.Add(delta)
		readErrors, writeErrors := uint64(0), uint64(0)
		if i == 2 {
			readErrors, writeErrors = delta.ReadOps/200, delta.WriteOps/300
		}
		addDemoDiagnostics(&s.diagnostics[i].Read, delta.ReadOps, readErrors, uint64(30000+float64(i)*60000+wave*300000))
		addDemoDiagnostics(&s.diagnostics[i].Write, delta.WriteOps, writeErrors, uint64(100000+float64(i)*100000+(1.3-wave)*1000000))
	}
	if at.After(s.last) {
		s.last = at
	}
	clients := make([]collect.Record, len(s.records))
	for i, r := range s.records {
		clients[i] = collect.Record{Client: r.Client, Server: r.Server, Transport: r.Transport, Counters: r.Counters}
	}
	return collect.Snapshot{At: at, Records: append([]collect.Record(nil), s.records...), Clients: clients, Diagnostics: append([]collect.Diagnostic(nil), s.diagnostics...)}
}

func addDemoDiagnostics(d *collect.OpDiagnostics, successes, failures, durationNS uint64) {
	calls := successes + failures
	d.Calls += calls
	d.Errors += failures
	d.TotalNS += calls * durationNS
	if calls > 0 {
		d.MaxNS = max(d.MaxNS, durationNS)
	}
	micros := (durationNS + 999) / 1000
	bucket := 0
	for bucket < 31 && (uint64(1)<<uint(bucket)) < micros {
		bucket++
	}
	d.Buckets[bucket] += calls
}

func (*demoSource) Close() error        { return nil }
func (*demoSource) Description() string { return "DEMO / synthetic workload; no kernel probes" }

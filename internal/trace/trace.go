// SPDX-License-Identifier: BSD-2-Clause
package trace

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/netip"
	"nfsmon/internal/collect"
	"nfsmon/internal/layout"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type tail struct {
	mu sync.Mutex
	s  string
}

func (t *tail) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.s += string(b)
	if len(t.s) > 8192 {
		t.s = t.s[len(t.s)-8192:]
	}
	return len(b), nil
}
func (t *tail) String() string { t.mu.Lock(); defer t.mu.Unlock(); return strings.TrimSpace(t.s) }

type frame struct {
	snapshot collect.Snapshot
	err      error
}
type Source struct {
	cmd    *exec.Cmd
	temp   string
	frames chan frame
	done   chan struct{}
	stop   chan struct{}
	once   sync.Once
	stderr tail
	layout layout.Layout
}

func Open(l layout.Layout, proto uint32, group string, interval time.Duration) (_ *Source, err error) {
	tool, err := exec.LookPath("bpftrace")
	if err != nil {
		return nil, fmt.Errorf("install bpftrace >= 0.16: %w", err)
	}
	f, err := os.CreateTemp("", "nfsmon-*.bt")
	if err != nil {
		return nil, err
	}
	path := f.Name()
	defer func() {
		if err != nil {
			os.Remove(path)
		}
	}()
	if _, err = io.WriteString(f, Script(l, proto, group, interval)); err != nil {
		f.Close()
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	s := &Source{temp: path, frames: make(chan frame, 1), done: make(chan struct{}), stop: make(chan struct{}), layout: l}
	s.cmd = exec.Command(tool, "-q", path)
	s.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	s.cmd.Env = os.Environ()
	// Respect a user's explicit memory limit; otherwise bound keys consistently with nfstop.
	if os.Getenv("BPFTRACE_MAP_KEYS_MAX") == "" {
		s.cmd.Env = append(s.cmd.Env, "BPFTRACE_MAP_KEYS_MAX=16384")
	}
	s.cmd.Stderr = &s.stderr
	stdout, err := s.cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = s.cmd.Start(); err != nil {
		return nil, err
	}
	go s.consume(stdout)
	return s, nil
}
func (s *Source) Description() string { return "bpftrace / " + s.layout.Source }
func (s *Source) Close() error {
	s.once.Do(func() {
		close(s.stop)
		if s.cmd.Process != nil {
			// Signal only our private process group, never the user's foreground shell.
			_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGINT)
			select {
			case <-s.done:
			case <-time.After(3 * time.Second):
				_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
				<-s.done
			}
		}
		os.Remove(s.temp)
	})
	return nil
}
func (s *Source) Read(ctx context.Context) (collect.Snapshot, error) {
	select {
	case <-ctx.Done():
		return collect.Snapshot{}, ctx.Err()
	case f, ok := <-s.frames:
		if !ok {
			return collect.Snapshot{}, fmt.Errorf("bpftrace exited: %s", s.stderr.String())
		}
		return f.snapshot, f.err
	}
}
func (s *Source) deliver(f frame) bool {
	select {
	case <-s.stop:
		return false
	default:
	}
	select {
	case s.frames <- f:
	default:
		// Snapshots are cumulative; replacing a queued frame loses no byte totals.
		select {
		case <-s.frames:
		default:
		}
		select {
		case s.frames <- f:
		case <-s.stop:
			return false
		}
	}
	return true
}
func (s *Source) consume(reader io.Reader) {
	defer close(s.done)
	defer close(s.frames)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 65536), 4<<20)
	active := false
	records := map[string]*collect.Record{}
	clients := map[string]*collect.Record{}
	diagnostics := map[string]*collect.Diagnostic{}
	health := map[string]uint64{}
	var parseErr error
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "NFSMON_BEGIN" {
			active = true
			records = map[string]*collect.Record{}
			clients = map[string]*collect.Record{}
			diagnostics = map[string]*collect.Diagnostic{}
			health = map[string]uint64{}
			continue
		}
		if line == "NFSMON_END" && active {
			snap := collect.Snapshot{At: time.Now()}
			for _, r := range records {
				snap.Records = append(snap.Records, *r)
			}
			for _, r := range clients {
				snap.Clients = append(snap.Clients, *r)
			}
			for _, d := range diagnostics {
				snap.Diagnostics = append(snap.Diagnostics, *d)
			}
			if health["failed"]+health["overwritten"]+health["badreads"] > 0 {
				snap.Health = fmt.Sprintf("failed_ops=%d missed_or_overwritten=%d missing_detail_or_count=%d; inspect tracefs kprobe_profile for missed returns", health["failed"], health["overwritten"], health["badreads"])
			}
			snap.CheckDiagnosticHealth()
			if !s.deliver(frame{snapshot: snap, err: parseErr}) {
				break
			}
			active = false
			continue
		}
		if !active || !strings.HasPrefix(line, "@") {
			continue
		}
		if err := parseLine(line, records, clients, diagnostics, health); err != nil {
			parseErr = err
		}
	}
	if err := scanner.Err(); err != nil {
		parseErr = err
	}
	// A parse failure must not leave a long-lived bpftrace process behind.
	if parseErr != nil && s.cmd.Process != nil {
		_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGINT)
	}
	err := s.cmd.Wait()
	select {
	case <-s.stop:
		return
	default:
	}
	if parseErr != nil {
		s.deliver(frame{err: parseErr})
	} else if err != nil {
		s.deliver(frame{err: fmt.Errorf("bpftrace: %w\n%s", err, s.stderr.String())})
	}
}
func parseLine(line string, records, clients map[string]*collect.Record, diagnostics map[string]*collect.Diagnostic, health map[string]uint64) error {
	for _, name := range []string{"failed", "overwritten", "badreads"} {
		prefix := "@" + name + ": "
		if strings.HasPrefix(line, prefix) {
			n, e := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, prefix)), 10, 64)
			if e != nil {
				return e
			}
			health[name] = n
			return nil
		}
	}
	left, right, ok := strings.Cut(line, "]: ")
	if !ok {
		return nil
	}
	name, key, ok := strings.Cut(left, "[")
	if !ok {
		return nil
	}
	switch name {
	case "@rb", "@wb", "@ro", "@wo", "@crb", "@cwb", "@cro", "@cwo", "@dc", "@de", "@dn", "@dm", "@dh":
	default:
		return nil
	}
	n, err := strconv.ParseUint(strings.TrimSpace(right), 10, 64)
	if err != nil {
		return err
	}
	fields := strings.Split(key, ",")
	for i := range fields {
		fields[i] = strings.TrimSpace(fields[i])
	}
	if strings.HasPrefix(name, "@d") {
		return parseDiagnostic(name, fields, n, diagnostics)
	}
	if strings.HasPrefix(name, "@c") {
		records = clients
		name = "@" + strings.TrimPrefix(name, "@c")
	}
	key = strings.Join(fields, ",")
	r := records[key]
	if r == nil {
		if len(fields) != 3 && len(fields) != 7 {
			return fmt.Errorf("unexpected bpftrace key: %s", key)
		}
		c, err := netip.ParseAddr(fields[0])
		if err != nil {
			return err
		}
		server, err := netip.ParseAddr(fields[1])
		if err != nil {
			return err
		}
		transport, err := strconv.ParseUint(fields[2], 10, 32)
		if err != nil {
			return err
		}
		r = &collect.Record{Client: c.Unmap(), Server: server.Unmap(), Transport: uint32(transport)}
		if len(fields) == 7 {
			vals := make([]uint64, 4)
			for i := range vals {
				bits := 32
				if i == 3 {
					bits = 64
				}
				n, e := strconv.ParseUint(fields[i+3], 10, bits)
				if e != nil {
					return e
				}
				vals[i] = n
			}
			r.UID = uint32(vals[0])
			r.GID = uint32(vals[1])
			r.Device = uint32(vals[2])
			r.Inode = vals[3]
		}
		records[key] = r
	}
	switch name {
	case "@rb":
		r.Counters.Read = n
	case "@wb":
		r.Counters.Write = n
	case "@ro":
		r.Counters.ReadOps = n
	case "@wo":
		r.Counters.WriteOps = n
	}
	return nil
}

func parseDiagnostic(name string, fields []string, n uint64, diagnostics map[string]*collect.Diagnostic) error {
	want := 4
	if name == "@dh" {
		want = 5
	}
	if len(fields) != want {
		return fmt.Errorf("unexpected diagnostic key: %v", fields)
	}
	client, err := netip.ParseAddr(fields[0])
	if err != nil {
		return err
	}
	server, err := netip.ParseAddr(fields[1])
	if err != nil {
		return err
	}
	proto, err := strconv.ParseUint(fields[2], 10, 32)
	if err != nil {
		return err
	}
	direction, err := strconv.ParseUint(fields[3], 10, 1)
	if err != nil {
		return err
	}
	key := strings.Join(fields[:3], ",")
	d := diagnostics[key]
	if d == nil {
		d = &collect.Diagnostic{Client: client.Unmap(), Server: server.Unmap(), Transport: uint32(proto)}
		diagnostics[key] = d
	}
	op := &d.Read
	if direction != 0 {
		op = &d.Write
	}
	switch name {
	case "@dc":
		op.Calls = n
	case "@de":
		op.Errors = n
	case "@dn":
		op.TotalNS = n
	case "@dm":
		op.MaxNS = n
	case "@dh":
		bucket, err := strconv.ParseUint(fields[4], 10, 5)
		if err != nil {
			return err
		}
		op.Buckets[bucket] = n
	}
	return nil
}

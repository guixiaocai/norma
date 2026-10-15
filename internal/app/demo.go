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
}

func newDemoSource() *demoSource {
	now := time.Now()
	s := &demoSource{start: now, last: now}
	for i := 0; i < 8; i++ {
		s.records = append(s.records, collect.Record{
			Client: netip.AddrFrom4([4]byte{192, 0, 2, byte(21 + i)}),
			Server: netip.MustParseAddr("192.0.2.10"),
			UID:    uint32(1000 + i%3), GID: 1000, Device: 2049,
			Inode: uint64(120000 + i), Name: fmt.Sprintf("projects/dataset-%02d.bin", i+1),
		})
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
		*c = c.Add(collect.Counters{Read: uint64(r * dt), Write: uint64(w * dt), ReadOps: uint64(r * dt / 65536), WriteOps: uint64(w * dt / 65536)})
	}
	if at.After(s.last) {
		s.last = at
	}
	return collect.Snapshot{At: at, Records: append([]collect.Record(nil), s.records...)}
}

func (*demoSource) Close() error        { return nil }
func (*demoSource) Description() string { return "DEMO / synthetic workload; no kernel probes" }

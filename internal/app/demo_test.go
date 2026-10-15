// SPDX-License-Identifier: MIT
package app

import (
	"context"
	"testing"
	"time"
)

func TestDemoCumulativeAndGrouping(t *testing.T) {
	src := newDemoSource()
	first := src.sample(src.start.Add(time.Second))
	second := src.sample(src.start.Add(2 * time.Second))
	for i, r := range second.Records {
		if r.Counters.Read <= first.Records[i].Counters.Read || r.Counters.Write <= first.Records[i].Counters.Write {
			t.Fatal("demo counters must grow without mutating earlier snapshots")
		}
	}
	for group, want := range map[string]int{"clients": 8, "servers": 1, "users": 3, "groups": 1, "files": 8} {
		h := NewHistory(src.start, 5*time.Minute)
		h.Add(first, Options{Group: group})
		h.Add(second, Options{Group: group})
		if len(h.Entities) != want || h.Total.Now.Read <= 0 {
			t.Errorf("invalid %s grouping: %d entries; read %f", group, len(h.Entities), h.Total.Now.Read)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := src.Read(ctx); err == nil {
		t.Fatal("demo ignored cancellation")
	}
}

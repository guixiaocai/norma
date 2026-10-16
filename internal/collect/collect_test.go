// SPDX-License-Identifier: MIT
package collect

import (
	"math"
	"strings"
	"testing"
)

func TestDiagnosticHistogramBounds(t *testing.T) {
	d := OpDiagnostics{Calls: 100, Errors: 3, TotalNS: 400000, MaxNS: 8500}
	d.Buckets[0], d.Buckets[2], d.Buckets[3], d.Buckets[4] = 50, 44, 4, 2
	for _, tc := range []struct {
		q    float64
		want uint64
	}{
		{0, 1000}, {.5, 1000}, {.94, 4000}, {.95, 8000}, {.99, 8500}, {1, 8500}, {2, 8500}, {-1, 1000}, {math.NaN(), 0},
	} {
		if got := d.PercentileNS(tc.q); got != tc.want {
			t.Errorf("q=%v: got %d, want %d", tc.q, got, tc.want)
		}
	}
	if got := d.AverageNS(); got != 4000 {
		t.Fatalf("average %v", got)
	}
	if got := (OpDiagnostics{}).AverageNS(); got != 0 {
		t.Fatalf("empty average %v", got)
	}
	if got := (OpDiagnostics{}).PercentileNS(.99); got != 0 {
		t.Fatalf("empty quantile %d", got)
	}
	zero := OpDiagnostics{Calls: 1}
	zero.Buckets[0] = 1
	if got := zero.PercentileNS(.99); got != 0 {
		t.Fatalf("zero duration %d", got)
	}
	tail := OpDiagnostics{Calls: 1, MaxNS: 9_000_000_000_000}
	tail.Buckets[31] = 1
	if got := tail.PercentileNS(.95); got != tail.MaxNS {
		t.Fatalf("saturated tail %d", got)
	}
}

func TestDiagnosticAggregationUsesHistogramPopulation(t *testing.T) {
	a := OpDiagnostics{Calls: 4, Errors: 1, TotalNS: 12000, MaxNS: 6000}
	a.Buckets[3] = 3
	b := OpDiagnostics{Calls: 2, Errors: 1, TotalNS: 8000, MaxNS: 4500}
	b.Buckets[3] = 2
	d := a.Add(b)
	if d.Calls != 6 || d.Errors != 2 || d.TotalNS != 20000 || d.MaxNS != 6000 || d.Buckets[3] != 5 {
		t.Fatalf("merged %+v", d)
	}
	// A missing bucket can come from a live read or a full histogram map.
	// Neither case may produce a percentile from a biased subset of calls.
	if d.HistogramComplete() || d.QuantileAvailable() || d.PercentileNS(.99) != 0 {
		t.Fatal("partial histogram was presented as a valid quantile")
	}
}

func TestDiagnosticAvailability(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		d                           OpDiagnostics
		complete, maximum, quantile bool
	}{
		{"empty", OpDiagnostics{}, false, false, false},
		{"complete", OpDiagnostics{Calls: 3, MaxNS: 3000, Buckets: [32]uint64{1, 0, 2}}, true, true, true},
		{"missing bucket", OpDiagnostics{Calls: 3, MaxNS: 3000, Buckets: [32]uint64{1, 0, 1}}, false, true, false},
		{"bucket ahead of calls", OpDiagnostics{Calls: 2, MaxNS: 3000, Buckets: [32]uint64{1, 0, 2}}, false, true, false},
		{"missing maximum", OpDiagnostics{Calls: 3, Buckets: [32]uint64{1, 0, 2}}, true, false, false},
		{"overflow cannot appear complete", OpDiagnostics{Calls: 1, MaxNS: 3000, Buckets: [32]uint64{math.MaxUint64, 2}}, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.d.HistogramComplete() != tc.complete || tc.d.MaxAvailable() != tc.maximum || tc.d.QuantileAvailable() != tc.quantile {
				t.Fatalf("availability for %+v", tc.d)
			}
			if !tc.quantile && tc.d.PercentileNS(.95) != 0 {
				t.Fatal("unavailable quantile was nonzero")
			}
		})
	}
	large := OpDiagnostics{Calls: math.MaxUint64, MaxNS: 500, Buckets: [32]uint64{math.MaxUint64}}
	if !large.HistogramComplete() || large.PercentileNS(1) != 500 {
		t.Fatal("valid maximum-size population overflowed")
	}
}

func TestSnapshotWarningsPreserveRawDiagnosticsAndRecover(t *testing.T) {
	bad := Diagnostic{Read: OpDiagnostics{Calls: 2, MaxNS: 4000, Buckets: [32]uint64{1}}, Write: OpDiagnostics{Calls: 1, Buckets: [32]uint64{1}}}
	s := Snapshot{Health: "failed_ops=1", Diagnostics: []Diagnostic{bad}}
	s.CheckDiagnosticHealth()
	if s.Diagnostics[0] != bad || !strings.Contains(s.Health, "failed_ops=1; diagnostic_incomplete=1 max_unavailable=1") {
		t.Fatalf("warning/data: %+v", s)
	}
	good := bad
	good.Read.Buckets[0] = 2
	good.Write.MaxNS = 500
	next := Snapshot{Diagnostics: []Diagnostic{good}}
	next.CheckDiagnosticHealth()
	if next.Health != "" {
		t.Fatalf("coherent next snapshot retained transient warning: %s", next.Health)
	}
	bare := Snapshot{Diagnostics: []Diagnostic{{}}}
	bare.CheckDiagnosticHealth()
	if bare.Health != "" {
		t.Fatalf("empty directions warned: %s", bare.Health)
	}
}

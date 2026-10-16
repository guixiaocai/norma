// SPDX-License-Identifier: MIT
package app

import (
	"math"
	"net/netip"
	"strings"
	"testing"
	"time"

	"nfsmon/internal/collect"
)

func testRecord(client string, transport uint32, inode uint64, uid uint32, name string, counters collect.Counters) collect.Record {
	return collect.Record{Client: netip.MustParseAddr(client), Server: netip.MustParseAddr("192.0.2.10"), Transport: transport, Device: 1, Inode: inode, UID: uid, GID: 20, Name: name, Counters: counters}
}
func TestRowsSelectedRespectsMetricAndDirection(t *testing.T) {
	start := time.Unix(1000, 0)
	h := NewHistory(start, time.Minute)
	h.Last = start.Add(time.Second)
	a := Rate{Read: 100, Write: 1, ReadOps: 1, WriteOps: 80}
	b := Rate{Read: 10, Write: 200, ReadOps: 50, WriteOps: 2}
	for key, r := range map[string]Rate{"a": a, "b": b} {
		h.Entities[key] = &Entity{Key: key, Label: key, Now: r, Average1: r, AverageW: r, Total: collect.Counters{Read: uint64(r.Read), Write: uint64(r.Write), ReadOps: uint64(r.ReadOps), WriteOps: uint64(r.WriteOps)}, Segments: []Segment{{Start: start, End: h.Last, Rate: r}}}
	}
	for _, metric := range []string{"now", "1m", "window", "peak", "total"} {
		for _, ops := range []bool{false, true} {
			for direction := 0; direction < 4; direction++ {
				want := "b"
				if !ops && direction == 1 || ops && direction != 1 {
					want = "a"
				}
				if got := h.RowsSelected(metric, ops, direction)[0].Key; got != want {
					t.Errorf("%s ops=%v direction=%d: got %s want %s", metric, ops, direction, got, want)
				}
			}
		}
	}
	if h.Rows("now")[0].Key != "b" {
		t.Fatal("legacy Rows lost byte/both ordering")
	}
}
func TestCombinedPeakUsesSimultaneousSamples(t *testing.T) {
	start := time.Unix(1000, 0)
	h := NewHistory(start, time.Minute)
	h.Last = start.Add(2 * time.Second)
	h.Entities["a"] = &Entity{Key: "a", Label: "a", Segments: []Segment{{Start: start, End: start.Add(time.Second), Rate: Rate{Read: 100, ReadOps: 100}}, {Start: start.Add(time.Second), End: h.Last, Rate: Rate{Write: 100, WriteOps: 100}}}}
	h.Entities["b"] = &Entity{Key: "b", Label: "b", Segments: []Segment{{Start: start, End: h.Last, Rate: Rate{Read: 60, Write: 60, ReadOps: 60, WriteOps: 60}}}}
	for _, ops := range []bool{false, true} {
		if h.RowsSelected("peak", ops, 0)[0].Key != "b" || h.RowsSelected("peak", ops, 3)[0].Key != "b" {
			t.Fatal("combined peak summed independent directional maxima")
		}
		if h.RowsSelected("peak", ops, 1)[0].Key != "a" || h.RowsSelected("peak", ops, 2)[0].Key != "a" {
			t.Fatal("directional peak did not select that direction")
		}
	}
}
func TestHistoryRegroupAndFilterPreserveSession(t *testing.T) {
	start := time.Unix(1000, 0)
	o := Options{Group: "clients", Transport: "all", Nodes: map[string]Node{"192.0.2.21": {Name: "worker-a", Group: "compute"}, "192.0.2.22": {Name: "worker-b", Group: "storage"}}}
	h := NewHistory(start, 5*time.Minute)
	records := []collect.Record{testRecord("192.0.2.21", 6, 1, 10, "project/report.bin", collect.Counters{Read: 100, ReadOps: 2}), testRecord("192.0.2.22", 256, 1, 11, "project/report.bin", collect.Counters{Read: 60, Write: 50, ReadOps: 3, WriteOps: 5}), testRecord("192.0.2.21", 6, 2, 10, "project/image.bin", collect.Counters{Read: 40, Write: 20, ReadOps: 2, WriteOps: 2})}
	h.Add(collect.Snapshot{At: start.Add(time.Second), Records: records}, o)
	for i := range records {
		records[i].Counters = records[i].Counters.Add(records[i].Counters)
	}
	h.Add(collect.Snapshot{At: start.Add(2 * time.Second), Records: records}, o)
	if h.Total.Total.Read != 400 || h.Total.Total.Write != 140 {
		t.Fatal("incorrect baseline totals")
	}
	if label := h.Entities["192.0.2.21"].Label; label != "worker-a (192.0.2.21)" {
		t.Fatalf("alias lost original IP: %s", label)
	}
	for group, count := range map[string]int{"clients": 2, "files": 2, "users": 2, "groups": 1, "servers": 1, "transports": 2, "nodegroups": 2} {
		opts := o
		opts.Group = group
		p := h.Project(opts)
		if len(p.Entities) != count || p.Total.Total.Read != 400 || p.Total.Total.Write != 140 || !p.Start.Equal(start) {
			t.Errorf("%s projection changed session: %+v", group, p.Total)
		}
	}
	if h.Transports[6].Read != 140 || h.Transports[256].Read != 60 {
		t.Fatalf("transport map double-counted or lost attribution: %+v", h.Transports)
	}
	filtered := o
	filtered.FileFilter = "REPORT"
	p := h.Project(filtered)
	if p.Total.Total.Read != 320 || p.Total.Total.Write != 100 {
		t.Fatalf("case-insensitive filename hint filter: %+v", p.Total.Total)
	}
	prefix := netip.MustParsePrefix("192.0.2.21/32")
	filtered.Client = &prefix
	p = h.Project(filtered)
	if p.Total.Total.Read != 200 || p.Total.Total.Write != 0 || p.Total.AverageReadSize != 50 {
		t.Fatalf("client+filename intersection: %+v", p.Total)
	}
	filtered = o
	filtered.Transport = "tcp"
	p = h.Project(filtered)
	if p.Total.Total.Read != 280 || p.Transports[256] != (Rate{}) {
		t.Fatal("transport filter differs from transport strip")
	}
	if h.Total.Total.Read != 400 || len(h.Entities) != 2 {
		t.Fatal("Project mutated the original view")
	}
	if !h.Entities["192.0.2.21"].FirstSeen.Equal(start.Add(time.Second)) {
		t.Fatal("regrouping reset first observation")
	}
}
func TestClientSummariesSurviveDetailCapacityLimits(t *testing.T) {
	start := time.Unix(1000, 0)
	h := NewHistory(start, time.Minute)
	o := Options{Group: "clients"}
	detail := testRecord("192.0.2.21", 6, 1, 10, "report", collect.Counters{Read: 100, ReadOps: 1})
	client := detail
	client.Inode = 0
	client.Name = ""
	client.Counters.Read = 150
	h.Add(collect.Snapshot{At: start.Add(time.Second), Records: []collect.Record{detail}, Clients: []collect.Record{client}}, o)
	if h.Total.Total.Read != 150 {
		t.Fatal("clients use truncated detailed counters or count both maps")
	}
	o.Group = "files"
	if p := h.Project(o); p.Total.Total.Read != 100 {
		t.Fatal("file projection used endpoint summary")
	}
	o.Group = "clients"
	o.FileFilter = "report"
	if p := h.Project(o); p.Total.Total.Read != 100 {
		t.Fatal("filename-filtered clients must use matching detail only")
	}
}
func TestDiagnosticsOnlyClientsAndAggregation(t *testing.T) {
	start := time.Unix(1000, 0)
	h := NewHistory(start, time.Minute)
	o := Options{Group: "clients"}
	client := netip.MustParseAddr("192.0.2.25")
	server := netip.MustParseAddr("192.0.2.10")
	d := collect.Diagnostic{Client: client, Server: server, Transport: 6, Read: collect.OpDiagnostics{Calls: 2, Errors: 2, TotalNS: 4000, MaxNS: 3000, Buckets: [32]uint64{0, 1, 1}}}
	h.Add(collect.Snapshot{At: start.Add(time.Second), Clients: []collect.Record{}, Diagnostics: []collect.Diagnostic{d}}, o)
	h.Add(collect.Snapshot{At: start.Add(2 * time.Second), Clients: []collect.Record{}, Diagnostics: []collect.Diagnostic{d}}, o)
	e := h.Entities[client.String()]
	if e == nil || e.Diagnostics.Read.Errors != 2 || e.Total.Read != 0 {
		t.Fatal("failed-only client is invisible")
	}
	if !e.FirstSeen.Equal(start.Add(time.Second)) {
		t.Fatal("failed-only first observation changed")
	}
	if e.ReadLatency.AverageNS != 2000 || e.ReadLatency.P95UpperNS != 3000 || e.ReadLatency.P99UpperNS != 3000 {
		t.Fatalf("latency summaries missing or wrong: %+v", e.ReadLatency)
	}
	d2 := d
	d2.Client = netip.MustParseAddr("192.0.2.26")
	d2.Transport = 256
	h.Add(collect.Snapshot{At: start.Add(3 * time.Second), Clients: []collect.Record{}, Diagnostics: []collect.Diagnostic{d, d2}}, o)
	if h.Total.Diagnostics.Client.IsValid() || h.Total.Diagnostics.Transport != 0 || h.Total.Diagnostics.Read.Errors != 4 {
		t.Fatal("merged diagnostics attributed to the first endpoint")
	}
	o.Group = "files"
	p := h.Project(o)
	if p.Total.Diagnostics != nil || p.Total.ReadLatency != nil {
		t.Fatal("per-client latency falsely attributed to files")
	}
}
func TestHistoryIdleAndMissingRecords(t *testing.T) {
	start := time.Unix(1000, 0)
	h := NewHistory(start, time.Minute)
	o := Options{Group: "clients"}
	r := testRecord("192.0.2.21", 6, 1, 10, "report", collect.Counters{Read: 100, ReadOps: 1})
	h.Add(collect.Snapshot{At: start.Add(time.Second), Records: []collect.Record{r}}, o)
	h.Add(collect.Snapshot{At: start.Add(2 * time.Second)}, o)
	if h.Total.Total.Read != 100 || h.Total.Now.Read != 0 || h.Total.AverageW.Read != 50 {
		t.Fatalf("missing/idle snapshot reset totals or excluded idle time: %+v", h.Total)
	}
	if len(h.records[recordKey(r)].entity.Segments) != 1 {
		t.Fatal("idle samples allocate historical segments")
	}
}
func TestHistoryRetentionMarksUnavailableAndRecovers(t *testing.T) {
	start := time.Unix(1000, 0)
	h := NewHistory(start, 10*time.Second)
	o := Options{Group: "files"}
	r := testRecord("192.0.2.21", 6, 1, 10, "report", collect.Counters{Read: 120, ReadOps: 12})
	e := &Entity{Total: r.Counters, Now: Rate{Read: 10, ReadOps: 1}, FirstSeen: start.Add(time.Second)}
	for i := 0; i < 12; i++ {
		e.Segments = append(e.Segments, Segment{Start: start.Add(time.Duration(i) * time.Second), End: start.Add(time.Duration(i+1) * time.Second), Rate: Rate{Read: 10, ReadOps: 1}})
	}
	h.records[recordKey(r)] = &recordSeries{record: r, entity: e}
	h.Last = start.Add(12 * time.Second)
	limitSeries(h.records, 5)
	h.Reproject(o)
	if len(e.Segments) != 5 || h.Total.Average1Valid || h.Total.AverageWValid || h.Total.PeakWValid {
		t.Fatal("retention limit silently presented partial historical metrics")
	}
	if h.Total.Total.Read != 120 || h.Total.Now.Read != 10 || !strings.Contains(h.Health, "retention limit") {
		t.Fatal("retention changed exact counters or failed to warn")
	}
	if !h.Total.HistoryStart.Equal(start.Add(7 * time.Second)) {
		t.Fatal("retained boundary is wrong")
	}
	h.Add(collect.Snapshot{At: start.Add(100 * time.Second)}, o)
	if !h.Total.Average1Valid || !h.Total.AverageWValid || h.Total.AverageW.Read != 0 || strings.Contains(h.Health, "retention limit") {
		t.Fatal("metrics did not recover once missing history aged outside their windows")
	}
}
func TestHistoryPerSeriesRetentionBound(t *testing.T) {
	e := &Entity{}
	start := time.Unix(1000, 0)
	for i := 0; i < maxSeriesSegments+3; i++ {
		e.Segments = append(e.Segments, Segment{End: start.Add(time.Duration(i) * time.Second)})
	}
	series := map[string]*recordSeries{"test": {entity: e}}
	limitSeries(series, maxSeriesSegments*2)
	if len(e.Segments) != maxSeriesSegments || !e.HistoryStart.Equal(start.Add(2*time.Second)) {
		t.Fatal("per-series storage is not bounded")
	}
}
func TestProjectionAggregatesCoincidentSamplesAndSizes(t *testing.T) {
	start := time.Unix(1000, 0)
	h := NewHistory(start, time.Minute)
	o := Options{Group: "clients"}
	a := testRecord("192.0.2.21", 6, 1, 10, "a", collect.Counters{Read: 100, ReadOps: 2})
	b := testRecord("192.0.2.21", 6, 2, 10, "b", collect.Counters{})
	h.Add(collect.Snapshot{At: start.Add(time.Second), Records: []collect.Record{a, b}}, o)
	b.Counters = collect.Counters{Read: 100, ReadOps: 4}
	h.Add(collect.Snapshot{At: start.Add(2 * time.Second), Records: []collect.Record{a, b}}, o)
	e := h.Entities["192.0.2.21"]
	if e.PeakW.Read != 100 || e.Now.Read != 100 || e.AverageW.Read != 100 {
		t.Fatal("grouping summed independent historical peaks")
	}
	if math.Abs(e.AverageReadSize-200.0/6) > 1e-9 || e.Share != 100 {
		t.Fatalf("operation size/share invalid: %+v", e)
	}
}

func TestAggregatedDiagnosticAvailabilityCannotCancelSkews(t *testing.T) {
	start := time.Unix(1000, 0)
	h := NewHistory(start, time.Minute)
	o := Options{Group: "clients"}
	a := collect.Diagnostic{Client: netip.MustParseAddr("192.0.2.21"), Server: netip.MustParseAddr("192.0.2.10"), Transport: 6, Read: collect.OpDiagnostics{Calls: 2, TotalNS: 2000, MaxNS: 1000, Buckets: [32]uint64{3}}}
	b := a
	b.Server = netip.MustParseAddr("192.0.2.11")
	b.Read.Buckets[0] = 1
	h.Add(collect.Snapshot{At: start.Add(time.Second), Diagnostics: []collect.Diagnostic{a, b}}, o)
	e := h.Entities["192.0.2.21"]
	if !e.Diagnostics.Read.HistogramComplete() {
		t.Fatal("fixture should have cancelling raw bucket skews")
	}
	if e.ReadLatency.PercentilesAvailable || e.ReadLatency.HistogramComplete {
		t.Fatal("merged histogram falsely certified mismatched source histograms")
	}
	a.Read.Buckets[0], b.Read.Buckets[0] = 2, 2
	b.Read.MaxNS = 0
	h.Add(collect.Snapshot{At: start.Add(2 * time.Second), Diagnostics: []collect.Diagnostic{a, b}}, o)
	e = h.Entities["192.0.2.21"]
	if e.ReadLatency.MaxAvailable || e.ReadLatency.PercentilesAvailable {
		t.Fatal("merged max hid one endpoint's unavailable maximum")
	}
	line := latencyLine("READ", e.Diagnostics.Read, e.ReadLatency)
	if !strings.Contains(line, "p95—") || !strings.Contains(line, "max —") {
		t.Fatalf("incomplete aggregate rendered as observed latency: %s", line)
	}
}

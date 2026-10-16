// SPDX-License-Identifier: MIT
package app

import (
	"container/heap"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"nfsmon/internal/collect"
)

type Rate struct{ Read, Write, ReadOps, WriteOps float64 }

func rate(c collect.Counters, seconds float64) Rate {
	if seconds <= 0 {
		return Rate{}
	}
	return Rate{float64(c.Read) / seconds, float64(c.Write) / seconds, float64(c.ReadOps) / seconds, float64(c.WriteOps) / seconds}
}
func addRate(a, b Rate) Rate {
	return Rate{a.Read + b.Read, a.Write + b.Write, a.ReadOps + b.ReadOps, a.WriteOps + b.WriteOps}
}

type Segment struct {
	Start, End time.Time
	Rate       Rate
}
type Entity struct {
	Key, Label                               string
	Total                                    collect.Counters
	Segments                                 []Segment
	Now, Average1, AverageW, PeakW           Rate
	FirstSeen                                time.Time
	HistoryStart                             time.Time
	Average1Valid, AverageWValid, PeakWValid bool
	ReadLatency, WriteLatency                *LatencySummary `json:",omitempty"`
	AverageReadSize, AverageWriteSize        float64
	ReadShare, WriteShare, Share             float64
	Diagnostics                              *collect.Diagnostic `json:",omitempty"`

	diagnosticHistogramIncomplete, diagnosticMaxIncomplete, diagnosticAverageIncomplete [2]bool
}
type recordSeries struct {
	record collect.Record
	entity *Entity
}
type History struct {
	Start, Last      time.Time
	Window           time.Duration
	Entities         map[string]*Entity
	Total            *Entity
	Health           string
	Transports       map[uint32]Rate
	records, clients map[string]*recordSeries
	diagnostics      []collect.Diagnostic
	hasClients       bool
	collectorHealth  string
	diagnosticFirst  map[string]time.Time
	sorted           map[rowOrder][]*Entity
}

func NewHistory(start time.Time, w time.Duration) *History {
	return &History{Start: start, Last: start, Window: w, Entities: map[string]*Entity{}, Total: &Entity{Key: "TOTAL", Label: "TOTAL"}, records: map[string]*recordSeries{}, clients: map[string]*recordSeries{}, diagnosticFirst: map[string]time.Time{}}
}

// Retain the original record dimensions so runtime grouping and filtering never
// reinterpret old grouped totals. Idle intervals need no segment: averages use
// elapsed wall time, and sparse segments expire after the larger average window.
func (h *History) Add(s collect.Snapshot, o Options) {
	if !s.At.After(h.Last) {
		return
	}
	h.ingest(h.records, s.Records, s.At)
	if s.Clients != nil {
		h.hasClients = true
	}
	h.ingest(h.clients, s.Clients, s.At)
	h.diagnostics = append(h.diagnostics[:0], s.Diagnostics...)
	for _, d := range s.Diagnostics {
		if d.Read.Calls+d.Write.Calls == 0 {
			continue
		}
		key := recordKey(collect.Record{Client: d.Client, Server: d.Server, Transport: d.Transport})
		if h.diagnosticFirst[key].IsZero() {
			h.diagnosticFirst[key] = s.At
		}
	}
	limitSeries(h.records, 196608)
	limitSeries(h.clients, 65536)
	h.collectorHealth, h.Health, h.Last = s.Health, s.Health, s.At
	h.Reproject(o)
}
func recordKey(r collect.Record) string {
	return fmt.Sprintf("%s|%s|%d|%d|%d|%d|%d", r.Client.Unmap(), r.Server.Unmap(), r.Transport, r.UID, r.GID, r.Device, r.Inode)
}
func (h *History) ingest(series map[string]*recordSeries, records []collect.Record, at time.Time) {
	current := make(map[string]collect.Record, len(records))
	for _, r := range records {
		key := recordKey(r)
		if previous, ok := current[key]; ok {
			r.Counters = r.Counters.Add(previous.Counters)
		}
		current[key] = r
		if series[key] == nil {
			series[key] = &recordSeries{record: r, entity: &Entity{Key: key}}
		}
	}
	for key, item := range series {
		counters := item.entity.Total
		if r, ok := current[key]; ok {
			item.record, counters = r, r.Counters
		}
		h.advance(item.entity, counters, item.record.Name, at, at.Sub(h.Last).Seconds())
	}
}
func (h *History) Project(o Options) *History {
	projection := *h
	projection.Reproject(o)
	return &projection
}
func recordMatches(r collect.Record, o Options) bool {
	if len(o.Local) > 0 && !o.Local[r.Server.Unmap()] {
		return false
	}
	if o.Client != nil && !o.Client.Contains(r.Client.Unmap()) {
		return false
	}
	if o.Transport != "" && o.Transport != "all" && r.Transport != o.Protocol() {
		return false
	}
	return o.FileFilter == "" || strings.Contains(strings.ToLower(r.Name), strings.ToLower(o.FileFilter))
}
func summaryGroup(group string) bool {
	return group == "clients" || group == "servers" || group == "transports" || group == "nodegroups" || group == ""
}
func (h *History) Reproject(o Options) {
	h.Entities = map[string]*Entity{}
	h.sorted = nil
	h.Total = &Entity{Key: "TOTAL", Label: "TOTAL"}
	h.Health = h.collectorHealth
	h.Transports = map[uint32]Rate{}
	series := h.records
	if h.hasClients && summaryGroup(o.Group) && o.FileFilter == "" {
		series = h.clients
	}
	segments := map[*Entity]map[time.Time]Segment{}
	addEntity := func(target, source *Entity) {
		target.Total = target.Total.Add(source.Total)
		target.Now = addRate(target.Now, source.Now)
		if source.HistoryStart.After(target.HistoryStart) {
			target.HistoryStart = source.HistoryStart
		}
		if !source.FirstSeen.IsZero() && (target.FirstSeen.IsZero() || source.FirstSeen.Before(target.FirstSeen)) {
			target.FirstSeen = source.FirstSeen
		}
		if segments[target] == nil {
			segments[target] = map[time.Time]Segment{}
		}
		for _, segment := range source.Segments {
			value := segments[target][segment.End]
			value.Start, value.End = segment.Start, segment.End
			value.Rate = addRate(value.Rate, segment.Rate)
			segments[target][segment.End] = value
		}
	}
	for _, item := range series {
		if !recordMatches(item.record, o) {
			continue
		}
		h.Transports[item.record.Transport] = addRate(h.Transports[item.record.Transport], item.entity.Now)
		key, label := identityWithOptions(item.record, o)
		entity := h.Entities[key]
		if entity == nil {
			entity = &Entity{Key: key, Label: label}
			h.Entities[key] = entity
		}
		addEntity(entity, item.entity)
		addEntity(h.Total, item.entity)
	}
	finish := func(entity *Entity) {
		for _, segment := range segments[entity] {
			entity.Segments = append(entity.Segments, segment)
		}
		sort.Slice(entity.Segments, func(i, j int) bool { return entity.Segments[i].End.Before(entity.Segments[j].End) })
		entity.Average1, _ = h.window(entity, time.Minute, h.Last)
		entity.AverageW, entity.PeakW = h.window(entity, h.Window, h.Last)
		if entity.HistoryStart.Before(h.Start) {
			entity.HistoryStart = h.Start
		}
		oneStart := h.Last.Add(-time.Minute)
		if oneStart.Before(h.Start) {
			oneStart = h.Start
		}
		windowStart := h.Last.Add(-h.Window)
		if windowStart.Before(h.Start) {
			windowStart = h.Start
		}
		entity.Average1Valid = !oneStart.Before(entity.HistoryStart)
		entity.AverageWValid = !windowStart.Before(entity.HistoryStart)
		entity.PeakWValid = entity.AverageWValid
		if !entity.Average1Valid {
			entity.Average1 = Rate{}
		}
		if !entity.AverageWValid {
			entity.AverageW = Rate{}
			entity.PeakW = Rate{}
		}
		if entity.Diagnostics != nil {
			entity.ReadLatency = latencySummary(entity.Diagnostics.Read)
			entity.WriteLatency = latencySummary(entity.Diagnostics.Write)
			for i, summary := range []*LatencySummary{entity.ReadLatency, entity.WriteLatency} {
				summary.HistogramComplete = summary.HistogramComplete && !entity.diagnosticHistogramIncomplete[i]
				summary.MaxAvailable = summary.MaxAvailable && !entity.diagnosticMaxIncomplete[i]
				summary.AverageAvailable = summary.AverageAvailable && !entity.diagnosticAverageIncomplete[i]
				summary.PercentilesAvailable = summary.PercentilesAvailable && summary.HistogramComplete && summary.MaxAvailable
				if !summary.PercentilesAvailable {
					summary.P95UpperNS, summary.P99UpperNS = 0, 0
				}
				if !summary.MaxAvailable {
					summary.MaxNS = 0
				}
				if !summary.AverageAvailable {
					summary.AverageNS = 0
				}
			}
		}
		if entity.Total.ReadOps > 0 {
			entity.AverageReadSize = float64(entity.Total.Read) / float64(entity.Total.ReadOps)
		}
		if entity.Total.WriteOps > 0 {
			entity.AverageWriteSize = float64(entity.Total.Write) / float64(entity.Total.WriteOps)
		}
		if h.Total.Now.Read > 0 {
			entity.ReadShare = entity.Now.Read / h.Total.Now.Read * 100
		}
		if h.Total.Now.Write > 0 {
			entity.WriteShare = entity.Now.Write / h.Total.Now.Write * 100
		}
		if total := h.Total.Now.Read + h.Total.Now.Write; total > 0 {
			entity.Share = (entity.Now.Read + entity.Now.Write) / total * 100
		}
	}
	// Latency belongs to endpoint summaries only. A filename/UID grouping cannot
	// claim per-file latency from a per-client histogram.
	if summaryGroup(o.Group) && o.FileFilter == "" {
		for _, diagnostic := range h.diagnostics {
			r := collect.Record{Client: diagnostic.Client, Server: diagnostic.Server, Transport: diagnostic.Transport}
			if !recordMatches(r, o) {
				continue
			}
			key, label := identityWithOptions(r, o)
			entity := h.Entities[key]
			if entity == nil {
				entity = &Entity{Key: key, Label: label}
				h.Entities[key] = entity
			}
			seen := h.diagnosticFirst[recordKey(r)]
			for _, e := range []*Entity{entity, h.Total} {
				mergeDiagnostic(e, diagnostic)
				if !seen.IsZero() && (e.FirstSeen.IsZero() || seen.Before(e.FirstSeen)) {
					e.FirstSeen = seen
				}
			}
		}
	}
	for _, entity := range h.Entities {
		finish(entity)
	}
	finish(h.Total)
	if !h.Total.Average1Valid || !h.Total.AverageWValid {
		warning := "history retention limit: incomplete averages/peaks shown as —; NOW and session totals remain exact"
		if h.Health != "" {
			h.Health += "; "
		}
		h.Health += warning
	}
}
func mergeDiagnostic(e *Entity, d collect.Diagnostic) {
	for i, source := range []collect.OpDiagnostics{d.Read, d.Write} {
		active := source.Calls > 0 || source.Errors > 0 || source.TotalNS > 0 || source.MaxNS > 0
		for _, n := range source.Buckets {
			active = active || n > 0
		}
		if active {
			e.diagnosticHistogramIncomplete[i] = e.diagnosticHistogramIncomplete[i] || !source.HistogramComplete()
			e.diagnosticMaxIncomplete[i] = e.diagnosticMaxIncomplete[i] || !source.MaxAvailable()
			e.diagnosticAverageIncomplete[i] = e.diagnosticAverageIncomplete[i] || source.Calls == 0
		}
	}
	if e.Diagnostics == nil {
		e.Diagnostics = &collect.Diagnostic{Client: d.Client, Server: d.Server, Transport: d.Transport}
	}
	if e.Diagnostics.Client != d.Client {
		e.Diagnostics.Client = collect.Diagnostic{}.Client
	}
	if e.Diagnostics.Server != d.Server {
		e.Diagnostics.Server = collect.Diagnostic{}.Server
	}
	if e.Diagnostics.Transport != d.Transport {
		e.Diagnostics.Transport = 0
	}
	merge := func(target *collect.OpDiagnostics, source collect.OpDiagnostics) {
		target.Calls += source.Calls
		target.Errors += source.Errors
		target.TotalNS += source.TotalNS
		target.MaxNS = max(target.MaxNS, source.MaxNS)
		for i := range target.Buckets {
			target.Buckets[i] += source.Buckets[i]
		}
	}
	merge(&e.Diagnostics.Read, d.Read)
	merge(&e.Diagnostics.Write, d.Write)
}
func identity(r collect.Record, group string) (string, string) {
	return identityWithOptions(r, Options{Group: group})
}
func identityWithOptions(r collect.Record, o Options) (string, string) {
	switch o.Group {
	case "users":
		x := fmt.Sprintf("%d", r.UID)
		return x, "uid:" + x
	case "groups":
		x := fmt.Sprintf("%d", r.GID)
		return x, "gid:" + x
	case "servers":
		return r.Server.String(), r.Server.String()
	case "transports":
		x := fmt.Sprintf("%d", r.Transport)
		label := map[uint32]string{6: "TCP", 17: "UDP", 256: "RDMA"}[r.Transport]
		if label == "" {
			label = "UNKNOWN (" + x + ")"
		}
		return x, label
	case "nodegroups":
		group := o.Nodes[r.Client.Unmap().String()].Group
		if group == "" {
			group = "ungrouped"
		}
		return group, group
	case "files":
		x := fmt.Sprintf("%x:%d", r.Device, r.Inode)
		label := x
		if strings.Trim(r.Name, "/") != "" {
			label += " " + r.Name
		}
		return x, label
	default:
		x := r.Client.Unmap().String()
		label := x
		if node := o.Nodes[x]; node.Name != "" {
			label = node.Name + " (" + x + ")"
		}
		return x, label
	}
}
func (h *History) advance(e *Entity, current collect.Counters, label string, end time.Time, elapsed float64) {
	if label != "" {
		e.Label = label
	}
	e.Now = rate(current.Delta(e.Total), elapsed)
	e.Total = current
	if e.FirstSeen.IsZero() && (current.Read > 0 || current.Write > 0 || current.ReadOps > 0 || current.WriteOps > 0) {
		e.FirstSeen = end
	}
	if e.Now != (Rate{}) {
		e.Segments = append(e.Segments, Segment{h.Last, end, e.Now})
	}
	cutoff := end.Add(-max(h.Window, time.Minute))
	n := 0
	for n < len(e.Segments) && !e.Segments[n].End.After(cutoff) {
		n++
	}
	if n > 0 {
		e.Segments = append([]Segment(nil), e.Segments[n:]...)
	}
	// Window summaries are computed once on the grouped projection, not for
	// every retained raw series as well.
}
func (h *History) window(e *Entity, w time.Duration, now time.Time) (avg, peak Rate) {
	start := now.Add(-w)
	if start.Before(h.Start) {
		start = h.Start
	}
	seconds := now.Sub(start).Seconds()
	if seconds <= 0 {
		return
	}
	for _, s := range e.Segments {
		a, b := s.Start, s.End
		if a.Before(start) {
			a = start
		}
		if b.After(now) {
			b = now
		}
		dt := b.Sub(a).Seconds()
		if dt <= 0 {
			continue
		}
		avg.Read += s.Rate.Read * dt / seconds
		avg.Write += s.Rate.Write * dt / seconds
		avg.ReadOps += s.Rate.ReadOps * dt / seconds
		avg.WriteOps += s.Rate.WriteOps * dt / seconds
		peak.Read = math.Max(peak.Read, s.Rate.Read)
		peak.Write = math.Max(peak.Write, s.Rate.Write)
		peak.ReadOps = math.Max(peak.ReadOps, s.Rate.ReadOps)
		peak.WriteOps = math.Max(peak.WriteOps, s.Rate.WriteOps)
	}
	return
}
func selectedRate(r Rate, ops bool, direction int) float64 {
	rd, wr := r.Read, r.Write
	if ops {
		rd, wr = r.ReadOps, r.WriteOps
	}
	if direction == 1 {
		return rd
	}
	if direction == 2 {
		return wr
	}
	return rd + wr
}
func (h *History) selectedValue(e *Entity, sortBy string, ops bool, direction int) float64 {
	switch sortBy {
	case "1m":
		return selectedRate(e.Average1, ops, direction)
	case "window":
		return selectedRate(e.AverageW, ops, direction)
	case "peak":
		peak := 0.0
		for _, s := range e.Segments {
			if s.End.After(h.Last.Add(-h.Window)) {
				peak = max(peak, selectedRate(s.Rate, ops, direction))
			}
		}
		return peak
	case "total":
		return selectedRate(Rate{float64(e.Total.Read), float64(e.Total.Write), float64(e.Total.ReadOps), float64(e.Total.WriteOps)}, ops, direction)
	default:
		return selectedRate(e.Now, ops, direction)
	}
}
func (h *History) Rows(sortBy string) []*Entity { return h.RowsSelected(sortBy, false, 0) }

type rowOrder struct {
	metric    string
	ops       bool
	direction int
}

func (h *History) RowsSelected(sortBy string, ops bool, direction int) []*Entity {
	order := rowOrder{sortBy, ops, direction}
	if cached, ok := h.sorted[order]; ok {
		return cached
	}
	rows := make([]*Entity, 0, len(h.Entities))
	for _, e := range h.Entities {
		rows = append(rows, e)
	}
	sort.Slice(rows, func(i, j int) bool {
		if sortBy != "name" {
			av, bv := metricAvailable(rows[i], sortBy), metricAvailable(rows[j], sortBy)
			if av != bv {
				return av
			}
			if !av {
				if rows[i].Label == rows[j].Label {
					return rows[i].Key < rows[j].Key
				}
				return rows[i].Label < rows[j].Label
			}
			a, b := h.selectedValue(rows[i], sortBy, ops, direction), h.selectedValue(rows[j], sortBy, ops, direction)
			if a != b {
				return a > b
			}
		}
		if rows[i].Label == rows[j].Label {
			return rows[i].Key < rows[j].Key
		}
		return rows[i].Label < rows[j].Label
	})
	if h.sorted == nil {
		h.sorted = map[rowOrder][]*Entity{}
	}
	h.sorted[order] = rows
	return rows
}

// Limits cap sparse time-series storage independently so detailed-file pressure
// cannot evict the independent endpoint summaries. Trimmed windows are marked
// unavailable instead of silently reporting incomplete averages or peaks.
const maxSeriesSegments = 4096

type segmentCursor struct {
	entity *Entity
	index  int
}
type segmentHeap []segmentCursor

func (h segmentHeap) Len() int { return len(h) }
func (h segmentHeap) Less(i, j int) bool {
	return h[i].entity.Segments[h[i].index].End.Before(h[j].entity.Segments[h[j].index].End)
}
func (h segmentHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *segmentHeap) Push(v any)   { *h = append(*h, v.(segmentCursor)) }
func (h *segmentHeap) Pop() any     { old := *h; v := old[len(old)-1]; *h = old[:len(old)-1]; return v }
func trimSegments(e *Entity, n int) {
	if n <= 0 {
		return
	}
	e.HistoryStart = e.Segments[n-1].End
	e.Segments = append([]Segment(nil), e.Segments[n:]...)
}
func limitSeries(series map[string]*recordSeries, budget int) {
	count := 0
	for _, item := range series {
		trimSegments(item.entity, max(0, len(item.entity.Segments)-maxSeriesSegments))
		count += len(item.entity.Segments)
	}
	if count <= budget {
		return
	}
	queue := segmentHeap{}
	for _, item := range series {
		if len(item.entity.Segments) > 0 {
			queue = append(queue, segmentCursor{entity: item.entity})
		}
	}
	heap.Init(&queue)
	dropped := map[*Entity]int{}
	for count > budget && queue.Len() > 0 {
		cursor := heap.Pop(&queue).(segmentCursor)
		cursor.index++
		dropped[cursor.entity] = cursor.index
		count--
		if cursor.index < len(cursor.entity.Segments) {
			heap.Push(&queue, cursor)
		}
	}
	for entity, n := range dropped {
		trimSegments(entity, n)
	}
}
func metricAvailable(e *Entity, metric string) bool {
	if e.HistoryStart.IsZero() {
		return true
	}
	switch metric {
	case "1m":
		return e.Average1Valid
	case "window":
		return e.AverageWValid
	case "peak":
		return e.PeakWValid
	}
	return true
}

type LatencySummary struct {
	Calls, Errors                                                           uint64
	AverageNS                                                               float64
	P95UpperNS, P99UpperNS, MaxNS                                           uint64
	AverageAvailable, PercentilesAvailable, MaxAvailable, HistogramComplete bool
}

func latencySummary(d collect.OpDiagnostics) *LatencySummary {
	return &LatencySummary{Calls: d.Calls, Errors: d.Errors, AverageNS: d.AverageNS(), P95UpperNS: d.PercentileNS(.95), P99UpperNS: d.PercentileNS(.99), MaxNS: d.MaxNS, AverageAvailable: d.Calls > 0, PercentilesAvailable: d.QuantileAvailable(), MaxAvailable: d.MaxAvailable(), HistogramComplete: d.HistogramComplete()}
}

func human(v float64, bits, perSecond bool) string {
	unit := "B"
	base := 1024.0
	prefix := []string{"", "Ki", "Mi", "Gi", "Ti", "Pi"}
	if bits {
		v *= 8
		unit = "bit"
		base = 1000
		prefix = []string{"", "k", "M", "G", "T", "P"}
	}
	n := 0
	for v >= base && n < len(prefix)-1 {
		v /= base
		n++
	}
	suffix := ""
	if perSecond {
		suffix = "/s"
	}
	if v >= 100 || n == 0 {
		return fmt.Sprintf("%.0f%s%s%s", v, prefix[n], unit, suffix)
	}
	return fmt.Sprintf("%.2f%s%s%s", v, prefix[n], unit, suffix)
}
func humanOps(v float64, perSecond bool) string {
	unit := "ops"
	if perSecond {
		unit += "/s"
	}
	prefix := []string{"", "k", "M", "G", "T"}
	i := 0
	for v >= 1000 && i < len(prefix)-1 {
		v /= 1000
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f%s", v, unit)
	}
	return fmt.Sprintf("%.2f%s%s", v, prefix[i], unit)
}

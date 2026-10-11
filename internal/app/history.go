// SPDX-License-Identifier: MIT
package app

import (
	"fmt"
	"math"
	"nfsmon/internal/collect"
	"sort"
	"strings"
	"time"
)

type Rate struct{ Read, Write, ReadOps, WriteOps float64 }

func rate(c collect.Counters, seconds float64) Rate {
	if seconds <= 0 {
		return Rate{}
	}
	return Rate{float64(c.Read) / seconds, float64(c.Write) / seconds, float64(c.ReadOps) / seconds, float64(c.WriteOps) / seconds}
}

type Segment struct {
	Start, End time.Time
	Rate       Rate
}
type Entity struct {
	Key, Label                     string
	Total                          collect.Counters
	Segments                       []Segment
	Now, Average1, AverageW, PeakW Rate
}
type History struct {
	Start, Last time.Time
	Window      time.Duration
	Entities    map[string]*Entity
	Total       *Entity
	Health      string
}

func NewHistory(start time.Time, w time.Duration) *History {
	return &History{Start: start, Last: start, Window: w, Entities: map[string]*Entity{}, Total: &Entity{Key: "TOTAL", Label: "TOTAL"}}
}
func (h *History) Add(s collect.Snapshot, o Options) {
	if !s.At.After(h.Last) {
		return
	}
	totals := map[string]collect.Counters{}
	labels := map[string]string{}
	for _, r := range s.Records {
		if len(o.Local) > 0 && !o.Local[r.Server.Unmap()] {
			continue
		}
		if o.Client != nil && !o.Client.Contains(r.Client.Unmap()) {
			continue
		}
		k, label := identity(r, o.Group)
		totals[k] = totals[k].Add(r.Counters)
		labels[k] = label
	}
	for k := range totals {
		if h.Entities[k] == nil {
			h.Entities[k] = &Entity{Key: k, Label: labels[k]}
		}
	}
	elapsed := s.At.Sub(h.Last).Seconds()
	var grand collect.Counters
	for k, e := range h.Entities {
		current, ok := totals[k]
		if !ok {
			current = e.Total
		} // Do not invent a reset when an asynchronous snapshot lacks a key.
		h.advance(e, current, labels[k], s.At, elapsed)
		grand = grand.Add(e.Total)
	}
	h.advance(h.Total, grand, "TOTAL", s.At, elapsed)
	h.Health = s.Health
	h.Last = s.At
}
func identity(r collect.Record, group string) (string, string) {
	switch group {
	case "users":
		x := fmt.Sprintf("%d", r.UID)
		return x, "uid:" + x
	case "groups":
		x := fmt.Sprintf("%d", r.GID)
		return x, "gid:" + x
	case "servers":
		return r.Server.String(), r.Server.String()
	case "files":
		x := fmt.Sprintf("%x:%d", r.Device, r.Inode)
		label := x
		if strings.Trim(r.Name, "/") != "" {
			label += " " + r.Name
		}
		return x, label
	default:
		return r.Client.String(), r.Client.String()
	}
}
func (h *History) advance(e *Entity, current collect.Counters, label string, end time.Time, elapsed float64) {
	if label != "" {
		e.Label = label
	}
	e.Now = rate(current.Delta(e.Total), elapsed)
	e.Total = current
	e.Segments = append(e.Segments, Segment{h.Last, end, e.Now})
	retention := max(h.Window, time.Minute)
	cutoff := end.Add(-retention)
	n := 0
	for n < len(e.Segments) && !e.Segments[n].End.After(cutoff) {
		n++
	}
	if n > 0 {
		// Copy occasionally to bound backing-array retention during long-running sessions.
		e.Segments = append([]Segment(nil), e.Segments[n:]...)
	}
	e.Average1, _ = h.window(e, time.Minute, end)
	e.AverageW, e.PeakW = h.window(e, h.Window, end)
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
func (h *History) Rows(sortBy string) []*Entity {
	rows := make([]*Entity, 0, len(h.Entities))
	for _, e := range h.Entities {
		rows = append(rows, e)
	}
	value := func(e *Entity) float64 {
		switch sortBy {
		case "1m":
			return e.Average1.Read + e.Average1.Write
		case "window":
			return e.AverageW.Read + e.AverageW.Write
		case "peak":
			peak := 0.0
			start := h.Last.Add(-h.Window)
			for _, s := range e.Segments {
				if s.End.After(start) {
					peak = max(peak, s.Rate.Read+s.Rate.Write)
				}
			}
			return peak
		case "total":
			return float64(e.Total.Read + e.Total.Write)
		}
		return e.Now.Read + e.Now.Write
	}
	sort.Slice(rows, func(i, j int) bool {
		if sortBy == "name" {
			return rows[i].Label < rows[j].Label
		}
		a, b := value(rows[i]), value(rows[j])
		if a == b {
			return rows[i].Label < rows[j].Label
		}
		return a > b
	})
	return rows
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

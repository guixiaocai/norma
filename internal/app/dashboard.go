// SPDX-License-Identifier: MIT
package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"nfsmon/internal/collect"
)

// The dashboard measures terminal cells, not bytes or rune counts: CJK file
// names and ANSI styling must never push a column off the alternate screen.
type dashboardTheme struct{ color bool }

func (p dashboardTheme) paint(s, code string) string {
	if !p.color || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}
func (p dashboardTheme) dim(s string) string    { return p.paint(s, "38;5;245") }
func (p dashboardTheme) border(s string) string { return p.paint(s, "38;5;60") }
func (p dashboardTheme) read(s string) string   { return p.paint(s, "1;38;5;80") }
func (p dashboardTheme) write(s string) string  { return p.paint(s, "1;38;5;221") }
func (p dashboardTheme) strong(s string) string { return p.paint(s, "1") }

func fitCell(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = ansi.Truncate(s, width, "…")
	return s + strings.Repeat(" ", max(0, width-ansi.StringWidth(s)))
}
func rightCell(s string, width int) string {
	s = ansi.Truncate(s, width, "…")
	return strings.Repeat(" ", max(0, width-ansi.StringWidth(s))) + s
}
func joinedLine(left, right string, width int) string {
	if right == "" || ansi.StringWidth(right)+3 >= width {
		return fitCell(left, width)
	}
	return fitCell(left, width-ansi.StringWidth(right)-1) + " " + right
}
func (p dashboardTheme) edge(title string, width int, bottom bool) string {
	left, right := "╭", "╮"
	if bottom {
		left, right = "╰", "╯"
	}
	if width < 2 {
		return fitCell("", width)
	}
	if title != "" && width > 6 {
		title = " " + ansi.Truncate(title, width-6, "…") + " "
	}
	return p.border(left+"─") + title + p.border(strings.Repeat("─", max(0, width-3-ansi.StringWidth(title)))+right)
}
func (p dashboardTheme) inside(s string, width int) string {
	return p.border("│") + " " + fitCell(s, width-4) + " " + p.border("│")
}
func (p dashboardTheme) box(title string, lines []string, width int) []string {
	result := []string{p.edge(title, width, false)}
	for _, line := range lines {
		result = append(result, p.inside(line, width))
	}
	return append(result, p.edge("", width, true))
}

func cloneHistory(h *History) *History {
	copyEntity := func(e *Entity) *Entity {
		c := *e
		c.Segments = append([]Segment(nil), e.Segments...)
		return &c
	}
	c := *h
	c.sorted = nil
	c.Entities = make(map[string]*Entity, len(h.Entities))
	for key, entity := range h.Entities {
		c.Entities[key] = copyEntity(entity)
	}
	c.Total = copyEntity(h.Total)
	copySeries := func(series map[string]*recordSeries) map[string]*recordSeries {
		result := make(map[string]*recordSeries, len(series))
		for key, item := range series {
			result[key] = &recordSeries{record: item.record, entity: copyEntity(item.entity)}
		}
		return result
	}
	c.records, c.clients = copySeries(h.records), copySeries(h.clients)
	c.diagnostics = append([]collect.Diagnostic(nil), h.diagnostics...)
	c.diagnosticFirst = map[string]time.Time{}
	for key, at := range h.diagnosticFirst {
		c.diagnosticFirst[key] = at
	}
	c.Transports = map[uint32]Rate{}
	for protocol, rates := range h.Transports {
		c.Transports[protocol] = rates
	}
	return &c
}

func (m model) number(v float64, perSecond bool) string {
	if m.opts.Ops {
		return humanOps(v, perSecond)
	}
	return human(v, m.opts.Bits, perSecond)
}
func (m model) availableNumber(e *Entity, metric string, v float64, perSecond bool) string {
	if !metricAvailable(e, metric) {
		return "—"
	}
	return m.number(v, perSecond)
}
func (m model) transportSummary(width int) string {
	parts := []string{}
	for _, protocol := range []uint32{6, 256, 17} {
		label := map[uint32]string{6: "TCP", 256: "RDMA", 17: "UDP"}[protocol]
		rate := m.history.Transports[protocol]
		if width >= 120 && m.direction == 0 {
			read, write := rate.Read, rate.Write
			if m.opts.Ops {
				read, write = rate.ReadOps, rate.WriteOps
			}
			parts = append(parts, label+" R "+m.number(read, true)+" W "+m.number(write, true))
		} else {
			parts = append(parts, label+" "+m.number(selectedRate(rate, m.opts.Ops, m.direction), true))
		}
	}
	var unknown Rate
	for protocol, rate := range m.history.Transports {
		if protocol != 6 && protocol != 256 && protocol != 17 {
			unknown = addRate(unknown, rate)
		}
	}
	if unknown != (Rate{}) {
		parts = append(parts, "OTHER "+m.number(selectedRate(unknown, m.opts.Ops, m.direction), true))
	}
	return "View mix · " + strings.Join(parts, " · ")
}
func (m model) units() string {
	if m.opts.Ops {
		return "operations/s"
	}
	if m.opts.Bits {
		return "bits/s"
	}
	return "bytes/s"
}
func (m model) status(p dashboardTheme) string {
	if m.paused {
		return p.write("PAUSED") + p.dim(" · sampling continues")
	}
	if m.opts.Demo {
		return p.write("DEMO · SYNTHETIC DATA")
	}
	return p.read("● LIVE")
}
func (m model) directionLabel() string {
	return []string{"READ + WRITE", "READ", "WRITE", "COMBINED"}[m.direction%4]
}
func (m model) observed() time.Duration {
	return max(time.Duration(0), min(m.history.Last.Sub(m.history.Start), m.opts.Window))
}
func shortDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%02ds", int(d/time.Minute), int(d/time.Second)%60)
}
func (m model) rateValues(e *Entity, write bool) (now, avg1, avgW, peak, total float64) {
	if m.opts.Ops {
		if write {
			return e.Now.WriteOps, e.Average1.WriteOps, e.AverageW.WriteOps, e.PeakW.WriteOps, float64(e.Total.WriteOps)
		}
		return e.Now.ReadOps, e.Average1.ReadOps, e.AverageW.ReadOps, e.PeakW.ReadOps, float64(e.Total.ReadOps)
	}
	if write {
		return e.Now.Write, e.Average1.Write, e.AverageW.Write, e.PeakW.Write, float64(e.Total.Write)
	}
	return e.Now.Read, e.Average1.Read, e.AverageW.Read, e.PeakW.Read, float64(e.Total.Read)
}
func (m model) summary(p dashboardTheme, width int) []string {
	cardWidth := (width - 4) / 3
	widths := []int{cardWidth, cardWidth, width - cardWidth*2 - 4}
	cards := make([][]string, 3)
	for i, write := range []bool{false, true} {
		now, avg, _, _, total := m.rateValues(m.history.Total, write)
		title, color := "CLIENT READ", p.read
		if write {
			title, color = "CLIENT WRITE", p.write
		}
		cards[i] = p.box(color(title), []string{
			color(m.number(now, true)),
			p.dim("1m avg  ") + m.availableNumber(m.history.Total, "1m", avg, true),
			p.dim("Session ") + m.number(total, false),
		}, widths[i])
	}
	cards[2] = p.box(p.strong("SESSION"), []string{
		p.strong(fmt.Sprintf("%d %s", len(m.history.Entities), m.opts.Group)),
		p.dim("Observed ") + shortDuration(m.observed()) + " / " + windowLabel(m.opts.Window),
		p.dim("Display  ") + m.units(),
	}, widths[2])
	lines := make([]string, len(cards[0]))
	for i := range lines {
		lines[i] = cards[0][i] + "  " + cards[1][i] + "  " + cards[2][i]
	}
	return lines
}

type metricColumn struct {
	title string
	index int
}

func (m model) columns(width int) []metricColumn {
	all := []metricColumn{{"NOW", 0}, {"AVG 1m", 1}, {"AVG " + windowLabel(m.opts.Window), 2}, {"PEAK " + windowLabel(m.opts.Window), 3}, {"SESSION", 4}}
	switch {
	case width >= 120:
		return all
	case width >= 100:
		return []metricColumn{all[0], all[1], all[3], all[4]}
	case width >= 78:
		return []metricColumn{all[0], all[2], all[4]}
	case width >= 56:
		return []metricColumn{all[0], all[4]}
	default:
		return all[:1]
	}
}
func (m model) table(p dashboardTheme, width, height int, spacious bool) []string {
	// Four reserved lines: title edge, headings, separator, bottom/range edge.
	contentHeight := max(0, height-4)
	columns := m.columns(width)
	innerWidth := width - 4
	metricWidth := 12
	nameWidth := max(1, innerWidth-7-len(columns)*(metricWidth+2))
	rowLine := func(name, dir string, metrics []string) string {
		line := fitCell(name, nameWidth) + "  " + fitCell(dir, 5)
		for _, metric := range metrics {
			line += "  " + rightCell(metric, metricWidth)
		}
		return p.inside(line, width)
	}
	headings := make([]string, len(columns))
	for i, column := range columns {
		headings[i] = p.dim(column.title)
	}
	title := p.strong(strings.ToUpper(m.opts.Group)) + p.dim("  ·  sort: "+m.opts.Sort+" / "+m.units()+"  ·  "+m.directionLabel())
	lines := []string{p.edge(title, width, false), rowLine(p.dim("ENTITY"), p.dim("I/O"), headings), p.inside(p.border(strings.Repeat("─", max(0, innerWidth))), width)}
	rows := m.rows()
	linesPer := 1
	if m.direction == 0 {
		linesPer = 2
	}
	gap := 0
	if spacious {
		gap = 1
	}
	room := (contentHeight + gap) / (linesPer + gap)
	offset := min(m.offset, max(0, len(rows)-max(1, room)))
	end := min(len(rows), offset+room)
	if len(rows) == 0 && contentHeight > 0 {
		lines = append(lines, p.inside(p.dim("Waiting for completed NFS reads / writes…"), width))
		if contentHeight > 1 {
			lines = append(lines, p.inside(p.dim("Generate NFS traffic; this view updates automatically."), width))
		}
	}
	for i, e := range rows[offset:end] {
		label := clean(e.Label)
		if !m.opts.Numeric && m.resolver != nil && !(m.opts.Group == "clients" && m.opts.Nodes[e.Key].Name != "") {
			label = m.resolver.label(e, m.opts.Group)
		}
		if m.opts.Group == "clients" || m.opts.Group == "servers" {
			suffix := " (" + e.Key + ")"
			if strings.HasSuffix(label, suffix) {
				room := nameWidth - 2 - ansi.StringWidth(suffix)
				if room > 0 {
					label = ansi.Truncate(strings.TrimSuffix(label, suffix), room, "…") + suffix
				} else {
					label = e.Key
				}
			}
		}
		if offset+i == m.cursor {
			label = p.strong("› " + label)
		} else {
			label = "  " + label
		}
		readNow, readAvg1, readAvgW, readPeak, readTotal := m.rateValues(e, false)
		writeNow, writeAvg1, writeAvgW, writePeak, writeTotal := m.rateValues(e, true)
		readValues := []float64{readNow, readAvg1, readAvgW, readPeak, readTotal}
		writeValues := []float64{writeNow, writeAvg1, writeAvgW, writePeak, writeTotal}
		add := func(dir string, values []float64, color func(string) string) {
			cells := make([]string, len(columns))
			for j, column := range columns {
				metric := []string{"now", "1m", "window", "peak", "total"}[column.index]
				cells[j] = m.availableNumber(e, metric, values[column.index], column.index != 4)
				if column.index == 0 {
					cells[j] = color(cells[j])
				}
			}
			lines = append(lines, rowLine(label, color(dir), cells))
			label = ""
		}
		if m.direction == 3 {
			values := make([]float64, 5)
			for j := range values {
				values[j] = readValues[j] + writeValues[j]
			}
			// A combined peak is a simultaneous sample, not a sum of peaks.
			values[3] = 0
			for _, segment := range e.Segments {
				if segment.End.After(m.history.Last.Add(-m.opts.Window)) {
					v := segment.Rate.Read + segment.Rate.Write
					if m.opts.Ops {
						v = segment.Rate.ReadOps + segment.Rate.WriteOps
					}
					values[3] = max(values[3], v)
				}
			}
			add("SUM", values, p.strong)
		} else {
			if m.direction == 0 || m.direction == 1 {
				add("READ", readValues, p.read)
			}
			if m.direction == 0 || m.direction == 2 {
				add("WRITE", writeValues, p.write)
			}
		}
		if gap > 0 && i < end-offset-1 {
			lines = append(lines, p.inside("", width))
		}
	}
	for len(lines) < height-1 {
		lines = append(lines, p.inside("", width))
	}
	first := min(len(rows), offset+1)
	if room == 0 {
		first, end = 0, 0
	}
	rangeText := fmt.Sprintf(" %d–%d of %d ", first, end, len(rows))
	if room < len(rows) {
		rangeText += "· ↑↓ select / PgDn page "
	}
	if len(columns) < 5 {
		rangeText += "· widen for more metrics "
	}
	return append(lines, p.edge(p.dim(strings.TrimSpace(rangeText)), width, true))
}

func (m model) health(p dashboardTheme) string {
	if m.opts.Demo && m.history.Health == "" {
		return p.write("DEMO") + p.dim(" · synthetic traffic; no kernel probes attached")
	}
	if m.history.Health != "" {
		return p.write("ATTENTION") + " · " + clean(m.history.Health)
	}
	if m.history.Last.Equal(m.history.Start) {
		return p.dim("WAITING · collector is starting; first sample pending")
	}
	return p.read("● OK") + p.dim(" · collector reported no errors or dropped records")
}

func (m model) healthLines(p dashboardTheme, width int) []string {
	lines := strings.Split(ansi.Wrap(m.health(p), width-4, ""), "\n")
	if len(lines) > 3 {
		lines = lines[:3]
		lines[2] = ansi.Truncate(lines[2], width-5, "") + "…"
	}
	return lines
}

func (m model) dimensions() (width, height int) {
	width, height = m.width, m.height
	if width <= 0 {
		width = 100
	}
	if height <= 0 {
		height = 30
	}
	return
}
func (m model) selectedEntity() *Entity {
	rows := m.rows()
	if len(rows) == 0 {
		return nil
	}
	return rows[min(max(0, m.cursor), len(rows)-1)]
}
func durationNS(v float64) string {
	if v >= 1e9 {
		return fmt.Sprintf("%.2fs", v/1e9)
	}
	if v >= 1e6 {
		return fmt.Sprintf("%.2fms", v/1e6)
	}
	if v >= 1e3 {
		return fmt.Sprintf("%.1fµs", v/1e3)
	}
	return fmt.Sprintf("%.0fns", v)
}
func (m model) selectedSummary() string {
	e := m.selectedEntity()
	if e == nil {
		return "Select a client with ↑↓; Enter opens its files, u switches to UID."
	}
	seen := "—"
	if !e.FirstSeen.IsZero() {
		seen = e.FirstSeen.Format("15:04:05")
	}
	share := 0.0
	if total := selectedRate(m.history.Total.Now, m.opts.Ops, m.direction); total > 0 {
		share = selectedRate(e.Now, m.opts.Ops, m.direction) / total * 100
	}
	return fmt.Sprintf("First %s · session avg/op R %s W %s · current share %.1f%%", seen, human(e.AverageReadSize, false, false), human(e.AverageWriteSize, false, false), share)
}
func latencyLine(label string, d collect.OpDiagnostics, summaries ...*LatencySummary) string {
	if d.Calls == 0 {
		return fmt.Sprintf("%s helper: no latency observations · errors %d / calls %d", label, d.Errors, d.Calls)
	}
	p95, p99, maximum := "—", "—", "—"
	summary := latencySummary(d)
	if len(summaries) > 0 && summaries[0] != nil {
		summary = summaries[0]
	}
	average := "—"
	if summary.AverageAvailable {
		average = durationNS(summary.AverageNS)
	}
	if summary.PercentilesAvailable {
		p95 = "≤" + durationNS(float64(d.PercentileNS(.95)))
		p99 = "≤" + durationNS(float64(d.PercentileNS(.99)))
	}
	if summary.MaxAvailable {
		maximum = durationNS(float64(d.MaxNS))
	}
	return fmt.Sprintf("%s helper avg %s  p95%s  p99%s  max %s  ·  errors %d/%d", label, average, p95, p99, maximum, d.Errors, d.Calls)
}
func (m model) selectionPanel(p dashboardTheme, width int) []string {
	e := m.selectedEntity()
	label := "SELECTED · SESSION STATS"
	lines := []string{p.dim(m.selectedSummary())}
	if e != nil {
		label += " · " + clean(e.Label)
		if e.Diagnostics != nil {
			lines = append(lines, p.read(latencyLine("READ", e.Diagnostics.Read, e.ReadLatency)), p.write(latencyLine("WRITE", e.Diagnostics.Write, e.WriteLatency)))
		} else {
			lines = append(lines, p.dim("Helper latency: unavailable for this view; client summaries carry endpoint diagnostics."))
		}
	}
	var wrapped []string
	for _, line := range lines {
		wrapped = append(wrapped, strings.Split(ansi.Wrap(line, width-4, ""), "\n")...)
	}
	return p.box(p.dim(label), wrapped, width)
}
func (m model) viewContext() string {
	parts := []string{}
	if m.detailClient != "" {
		parts = append(parts, "CLIENT DETAIL "+m.detailClient+" · u files/UID · Esc back")
	}
	if m.opts.Client != nil {
		parts = append(parts, "client="+m.opts.Client.String())
	}
	if m.opts.FileFilter != "" {
		parts = append(parts, "name contains "+clean(m.opts.FileFilter)+" (hint only)")
	}
	return strings.Join(parts, "  ·  ")
}
func (m model) filterPrompt(p dashboardTheme, width int) string {
	prompt := "/ " + clean(m.filterInput) + "▏"
	if m.filterError != "" {
		prompt += " · " + m.filterError
	} else if m.filterInput == "" {
		prompt += " IP/CIDR or file=substring · Enter apply · Esc cancel"
	}
	return p.write(ansi.Truncate(prompt, width, "…"))
}

// Layout the surrounding panels first, then give every remaining row to the
// entity table. Paging uses this identical budget, including host/health lines.
func (m model) frameParts(p dashboardTheme, width, height int) (before, after []string, spacious bool) {
	brand := p.read(strings.ToUpper(m.name)) + p.dim(" "+Version)
	status := m.status(p)
	if m.opts.Demo && m.paused {
		status = p.write("DEMO · PAUSED · sampling continues")
	}
	meta := fmt.Sprintf("NFS/%s  ·  %s  ·  sample %s  ·  window %s", strings.ToUpper(m.opts.Transport), m.opts.Backend, m.opts.Interval, windowLabel(m.opts.Window))
	if m.opts.Interface != "" {
		meta += "  ·  " + clean(m.opts.Interface)
	}
	context := m.viewContext()
	host := m.hostStats.Summary()
	if width >= 80 && height >= 28 {
		header := []string{joinedLine(brand, status+"  "+p.dim(m.history.Last.Format("15:04:05")), width-4), p.dim(meta)}
		header = append(header, p.dim(m.transportSummary(width-4)))
		if context != "" {
			header = append(header, p.write(context))
		}
		if host != "" {
			header = append(header, p.dim(host))
		}
		before = append(before, p.box(p.dim("NFS SERVER OBSERVABILITY"), header, width)...)
		before = append(before, "")
		before = append(before, m.summary(p, width)...)
		before = append(before, "")
		after = append(after, "")
		if width >= 100 && height >= 36 {
			after = append(after, m.selectionPanel(p, width)...)
			after = append(after, "")
		} else {
			after = append(after, p.dim(m.selectedSummary()))
		}
		after = append(after, p.box(p.dim("COLLECTOR HEALTH"), m.healthLines(p, width), width)...)
		after = append(after, p.dim(" ↑↓ select · Enter detail · d stats · u UID · g group · / filter · Esc back"))
		after = append(after, p.dim(" q quit · B units · o ops · s sort · t direction · T transport · p pause"))
		spacious = height >= 40
	} else {
		before = append(before, joinedLine(brand, status, width), p.dim(meta))
		if height >= 20 {
			before = append(before, p.dim(m.transportSummary(width)))
		}
		if context != "" && height >= 20 {
			before = append(before, p.write(context))
		}
		if host != "" && height >= 22 {
			before = append(before, p.dim(host))
		}
		read, _, _, _, _ := m.rateValues(m.history.Total, false)
		write, _, _, _, _ := m.rateValues(m.history.Total, true)
		before = append(before, "", joinedLine(p.read("READ "+m.number(read, true)), p.write("WRITE "+m.number(write, true)), width), p.dim(fmt.Sprintf("%d %s · observed %s · %s", len(m.history.Entities), m.opts.Group, shortDuration(m.observed()), m.units())), "")
		after = append(after, m.health(p))
		if width >= 60 {
			after = append(after, p.dim("↑↓ select · Enter detail · d stats · g group · / filter · Esc"), p.dim("q quit · B units · o ops · s sort · t dir · T proto · p pause"))
		} else {
			after = append(after, p.dim("q quit · ↑↓ select · d stats · Enter detail"))
		}
	}
	if m.filtering {
		after[len(after)-1] = m.filterPrompt(p, width)
	}
	return
}
func (m model) pageSize() int {
	width, height := m.dimensions()
	if width < 44 || height < 14 {
		return 0
	}
	if m.paused && m.frozen != nil {
		m.history = m.frozen
		m.hostStats = m.frozenHost
	}
	before, after, spacious := m.frameParts(dashboardTheme{}, width, height)
	gap := 0
	if spacious {
		gap = 1
	}
	linesPer := 1
	if m.direction == 0 {
		linesPer = 2
	}
	return max(0, height-len(before)-len(after)-4+gap) / (linesPer + gap)
}
func (m model) render() string {
	width, height := m.dimensions()
	p := dashboardTheme{color: !m.opts.NoColor && os.Getenv("NO_COLOR") == ""}
	var lines []string
	if m.diagnosticsView && width >= 44 && height >= 14 {
		return m.renderDiagnostics(p, width, height)
	}
	if width < 44 || height < 14 {
		lines = append(lines, joinedLine(p.read(strings.ToUpper(m.name))+p.dim(" "+Version), m.status(p), width))
		if m.opts.Demo {
			lines = append(lines, p.write("DEMO · SYNTHETIC DATA"))
		}
		if m.paused {
			lines = append(lines, p.write("PAUSED · sampling continues"))
		}
		read, _, _, _, _ := m.rateValues(m.history.Total, false)
		write, _, _, _, _ := m.rateValues(m.history.Total, true)
		lines = append(lines, "READ   "+m.number(read, true), "WRITE  "+m.number(write, true), fmt.Sprintf("%d %s · %s", len(m.history.Entities), m.opts.Group, m.history.Last.Format("15:04:05")), "", p.dim("Enlarge terminal for entity details."), m.health(p), p.dim("q quit · p pause · B units"))
		if m.filtering && len(lines) > 0 {
			lines[min(len(lines), height)-1] = m.filterPrompt(p, width)
		}
	} else {
		before, after, spacious := m.frameParts(p, width, height)
		tableHeight := height - len(before) - len(after)
		// Very long metadata can consume tiny viewports; hide optional header
		// lines instead of slicing away diagnostics and input feedback.
		for tableHeight < 4 && len(before) > 2 {
			before = before[:len(before)-1]
			tableHeight++
		}
		lines = append(lines, before...)
		lines = append(lines, m.table(p, width, tableHeight, spacious)...)
		lines = append(lines, after...)
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, line := range lines {
		lines[i] = fitCell(line, width)
	}
	return strings.Join(lines, "\n")
}

func (m model) renderDiagnostics(p dashboardTheme, width, height int) string {
	lines := p.box(p.dim("ENTITY DETAILS · SESSION STATISTICS"), []string{joinedLine(p.read(strings.ToUpper(m.name))+p.dim(" "+Version), m.status(p), width-4)}, width)
	lines = append(lines, "")
	lines = append(lines, m.selectionPanel(p, width)...)
	lines = append(lines, "")
	for _, note := range []string{"Helper latency is session-cumulative NFSD VFS return timing, including failures; it is not client RPC or storage-device latency.", "P95/P99 are histogram upper bounds. — means unavailable or incomplete observations."} {
		lines = append(lines, strings.Split(ansi.Wrap(p.dim(note), width, ""), "\n")...)
	}
	lines = append(lines, "", p.dim(m.transportSummary(width)))
	if host := m.hostStats.Summary(); host != "" {
		lines = append(lines, p.dim(host))
	}
	if len(lines) < height-1 {
		lines = append(lines, m.health(p))
	}
	for len(lines) < height-1 {
		lines = append(lines, "")
	}
	if len(lines) > height-1 {
		lines = lines[:height-1]
	}
	footer := p.dim("↑↓ select · d/Esc back · Enter detail · q quit")
	if m.filtering {
		footer = m.filterPrompt(p, width)
	}
	lines = append(lines, footer)
	for i, line := range lines {
		lines[i] = fitCell(line, width)
	}
	return strings.Join(lines, "\n")
}

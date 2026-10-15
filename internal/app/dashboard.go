// SPDX-License-Identifier: MIT
package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
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
	c.Entities = make(map[string]*Entity, len(h.Entities))
	for key, entity := range h.Entities {
		c.Entities[key] = copyEntity(entity)
	}
	c.Total = copyEntity(h.Total)
	return &c
}

func (m model) number(v float64, perSecond bool) string {
	if m.opts.Ops {
		return humanOps(v, perSecond)
	}
	return human(v, m.opts.Bits, perSecond)
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
			p.dim("1m avg  ") + m.number(avg, true),
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
	title := p.strong(strings.ToUpper(m.opts.Group)) + p.dim("  ·  sort: "+m.opts.Sort+"  ·  "+m.directionLabel())
	lines := []string{p.edge(title, width, false), rowLine(p.dim("ENTITY"), p.dim("I/O"), headings), p.inside(p.border(strings.Repeat("─", max(0, innerWidth))), width)}
	rows := m.history.Rows(m.opts.Sort)
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
		if !m.opts.Numeric && m.resolver != nil {
			label = m.resolver.label(e, m.opts.Group)
		}
		readNow, readAvg1, readAvgW, readPeak, readTotal := m.rateValues(e, false)
		writeNow, writeAvg1, writeAvgW, writePeak, writeTotal := m.rateValues(e, true)
		readValues := []float64{readNow, readAvg1, readAvgW, readPeak, readTotal}
		writeValues := []float64{writeNow, writeAvg1, writeAvgW, writePeak, writeTotal}
		add := func(dir string, values []float64, color func(string) string) {
			cells := make([]string, len(columns))
			for j, column := range columns {
				cells[j] = m.number(values[column.index], column.index != 4)
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
		rangeText += "· ↑↓ scroll "
	}
	if len(columns) < 5 {
		rangeText += "· widen for more metrics "
	}
	return append(lines, p.edge(p.dim(strings.TrimSpace(rangeText)), width, true))
}

func (m model) health(p dashboardTheme) string {
	if m.opts.Demo {
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

func (m model) pageSize() int {
	width, height := m.width, m.height
	if width <= 0 {
		width = 100
	}
	if height <= 0 {
		height = 30
	}
	if width < 44 || height < 14 {
		return 0
	}
	if m.paused && m.frozen != nil {
		m.history = m.frozen
	}
	tableHeight, gap := height-9, 0
	if width >= 80 && height >= 28 {
		tableHeight = height - 16 - len(m.healthLines(dashboardTheme{}, width))
		if height >= 34 {
			gap = 1
		}
	}
	linesPer := 1
	if m.direction == 0 {
		linesPer = 2
	}
	return max(0, tableHeight-4+gap) / (linesPer + gap)
}

func (m model) render() string {
	width, height := m.width, m.height
	if width <= 0 {
		width = 100
	}
	if height <= 0 {
		height = 30
	}
	p := dashboardTheme{color: !m.opts.NoColor && os.Getenv("NO_COLOR") == ""}
	lines := []string{}
	brand := p.read(strings.ToUpper(m.name)) + p.dim(" "+Version)
	status := m.status(p)
	if m.opts.Demo && m.paused {
		status = p.write("DEMO · PAUSED · sampling continues")
	}
	stamp := p.dim(m.history.Last.Format("15:04:05"))
	meta := fmt.Sprintf("NFS/%s  ·  %s  ·  sample %s  ·  window %s", strings.ToUpper(m.opts.Transport), m.opts.Backend, m.opts.Interval, windowLabel(m.opts.Window))
	if m.opts.Interface != "" {
		meta += "  ·  " + clean(m.opts.Interface)
	}
	if width < 44 || height < 14 {
		lines = append(lines, joinedLine(brand, status, width))
		if m.opts.Demo {
			lines = append(lines, p.write("DEMO · SYNTHETIC DATA"))
		}
		if m.paused {
			lines = append(lines, p.write("PAUSED · sampling continues"))
		}
		read, _, _, _, _ := m.rateValues(m.history.Total, false)
		write, _, _, _, _ := m.rateValues(m.history.Total, true)
		lines = append(lines, "READ   "+m.number(read, true), "WRITE  "+m.number(write, true), fmt.Sprintf("%d %s · %s", len(m.history.Entities), m.opts.Group, m.history.Last.Format("15:04:05")), "", p.dim("Enlarge terminal for entity details."), m.health(p), p.dim("q quit · p pause · B units"))
	} else if width >= 80 && height >= 28 {
		lines = append(lines, p.box(p.dim("NFS SERVER OBSERVABILITY"), []string{joinedLine(brand, status+"  "+stamp, width-4), p.dim(meta)}, width)...)
		lines = append(lines, "")
		lines = append(lines, m.summary(p, width)...)
		lines = append(lines, "")
		healthLines := m.healthLines(p, width)
		lines = append(lines, m.table(p, width, height-16-len(healthLines), height >= 34)...)
		lines = append(lines, "")
		lines = append(lines, p.box(p.dim("COLLECTOR HEALTH"), healthLines, width)...)
		if width >= 100 {
			lines = append(lines, p.dim(" READ / WRITE are client directions · averages include idle time · peak = maximum sample in window"))
		} else {
			lines = append(lines, p.dim(" READ / WRITE: client directions · averages include idle · peak: one sample"))
		}
		direction, scroll := "dir", "↑↓"
		if width >= 100 {
			direction, scroll = "direction", "↑↓ / PgUp PgDn"
		}
		lines = append(lines, " "+p.strong("q")+p.dim(" quit   ")+p.strong("B")+p.dim(" units   ")+p.strong("o")+p.dim(" ops   ")+p.strong("s")+p.dim(" sort   ")+p.strong("t")+p.dim(" "+direction+"   ")+p.strong("p")+p.dim(" pause   ")+p.strong(scroll)+p.dim(" scroll"))
	} else {
		lines = append(lines, joinedLine(brand, status, width), p.dim(meta))
		read, _, _, _, _ := m.rateValues(m.history.Total, false)
		write, _, _, _, _ := m.rateValues(m.history.Total, true)
		lines = append(lines, "", joinedLine(p.read("READ "+m.number(read, true)), p.write("WRITE "+m.number(write, true)), width), p.dim(fmt.Sprintf("%d %s  ·  observed %s  ·  %s", len(m.history.Entities), m.opts.Group, shortDuration(m.observed()), m.units())), "")
		lines = append(lines, m.table(p, width, height-9, false)...)
		keys := "q quit · B units · o ops · s sort · t dir · p pause · ↑↓"
		if width < 56 {
			keys = "q quit · p pause · ↑↓ scroll"
		}
		lines = append(lines, m.health(p), p.dim(keys), "")
	}
	// Every frame is bounded in both dimensions, including pause and resize.
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, line := range lines {
		lines[i] = fitCell(line, width)
	}
	return strings.Join(lines, "\n")
}

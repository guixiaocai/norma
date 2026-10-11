// SPDX-License-Identifier: MIT
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
	"nfsmon/internal/collect"
)

type tick struct {
	snapshot collect.Snapshot
	err      error
}
type model struct {
	ctx                              context.Context
	src                              collect.Source
	opts                             Options
	history                          *History
	name                             string
	width, height, offset, direction int
	frozen                           string
	paused                           bool
	err                              error
	resolver                         *resolver
}

func (m model) next() tea.Cmd {
	return tea.Tick(m.opts.Interval, func(_ time.Time) tea.Msg { s, e := m.src.Read(m.ctx); return tick{s, e} })
}
func (m model) Init() tea.Cmd { return m.next() }
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
	case tick:
		if v.err != nil {
			m.err = v.err
			return m, tea.Quit
		}
		m.history.Add(v.snapshot, m.opts)
		return m, m.next()
	case tea.KeyMsg:
		switch v.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "B", "b":
			m.opts.Bits = !m.opts.Bits
		case "o":
			m.opts.Ops = !m.opts.Ops
		case "s":
			order := []string{"now", "1m", "window", "peak", "total", "name"}
			for i, s := range order {
				if s == m.opts.Sort {
					m.opts.Sort = order[(i+1)%len(order)]
					break
				}
			}
		case "t":
			m.direction = (m.direction + 1) % 4
		case "p":
			if !m.paused {
				m.frozen = m.render()
			}
			m.paused = !m.paused
		case "up", "k":
			m.offset = max(0, m.offset-1)
		case "down", "j":
			m.offset++
		case "pgdown":
			m.offset += max(1, (m.height-9)/2)
		case "pgup":
			m.offset = max(0, m.offset-max(1, (m.height-9)/2))
		}
	}
	return m, nil
}
func (m model) View() string {
	if m.paused {
		lines := strings.Split(m.frozen, "\n")
		if len(lines) > 0 {
			lines[0] = ansi.Truncate("PAUSED (sampling continues) | "+lines[0], max(1, m.width), "")
		}
		return strings.Join(lines, "\n")
	}
	return m.render()
}
func windowLabel(d time.Duration) string {
	if d%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	return d.String()
}
func (m model) render() string {
	w := m.width
	if w <= 0 {
		w = 100
	}
	h := m.height
	if h <= 0 {
		h = 25
	}
	var b strings.Builder
	iface := ""
	if m.opts.Interface != "" {
		iface = " local=" + m.opts.Interface
	}
	fmt.Fprintf(&b, "%s %s | NFS/%s%s | %s | %s | sort=%s\n", m.name, Version, m.opts.Transport, iface, m.opts.Group, m.history.Last.Format("15:04:05"), m.opts.Sort)
	fmt.Fprintf(&b, "%-*s %-5s %12s %12s %12s %12s %12s\n", max(18, w-75), "CLIENT / ENTITY", "DIR", "NOW", "AVG 1m", "AVG "+windowLabel(m.opts.Window), "PEAK "+windowLabel(m.opts.Window), "TOTAL")
	rows := m.history.Rows(m.opts.Sort)
	linesPer := 2
	if m.direction != 0 {
		linesPer = 1
	}
	room := max(0, (h-10)/linesPer)
	offset := min(m.offset, max(0, len(rows)-max(1, room)))
	end := min(len(rows), offset+room)
	for _, e := range rows[offset:end] {
		m.entity(&b, e, w)
	}
	if len(rows) == 0 {
		b.WriteString("Waiting for matching NFS server read/write completions...\n")
	}
	fmt.Fprintf(&b, "%d clients/entities | showing %d-%d | observed %.0fs of %s window\n", len(rows), min(len(rows), offset+1), end, min(m.history.Last.Sub(m.history.Start).Seconds(), m.opts.Window.Seconds()), windowLabel(m.opts.Window))
	m.entity(&b, m.history.Total, w)
	fmt.Fprintf(&b, "PEAK: maximum %.3fs sample in window; averages include idle time. READ/WRITE are client directions.\n", m.opts.Interval.Seconds())
	if m.history.Health != "" {
		fmt.Fprintf(&b, "Health: %s\n", m.history.Health)
	}
	b.WriteString("q/Ctrl+C quit | B units | o ops | s sort | t directions | p pause | arrows scroll")
	// Keep the alternate screen inside the visible terminal. No clear-scrollback escape.
	lines := strings.Split(b.String(), "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, w, "")
	}
	return strings.Join(lines, "\n")
}
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, s)
}
func (m model) entity(b *strings.Builder, e *Entity, w int) {
	label := clean(e.Label)
	if !m.opts.Numeric {
		label = m.resolver.label(e, m.opts.Group)
	}
	label = ansi.Truncate(label, max(18, w-75), "…")
	line := func(dir string, now, a1, aw, peak float64, total uint64) {
		format := func(v float64, perSecond bool) string {
			if m.opts.Ops {
				return humanOps(v, perSecond)
			}
			return human(v, m.opts.Bits, perSecond)
		}
		fmt.Fprintf(b, "%-*s %-5s %12s %12s %12s %12s %12s\n", max(18, w-75), label, dir, format(now, true), format(a1, true), format(aw, true), format(peak, true), format(float64(total), false))
	}
	now, a1, aw, peak := e.Now, e.Average1, e.AverageW, e.PeakW
	rtotal, wtotal := e.Total.Read, e.Total.Write
	if m.opts.Ops {
		now.Read, now.Write = now.ReadOps, now.WriteOps
		a1.Read, a1.Write = a1.ReadOps, a1.WriteOps
		aw.Read, aw.Write = aw.ReadOps, aw.WriteOps
		peak.Read, peak.Write = peak.ReadOps, peak.WriteOps
		rtotal, wtotal = e.Total.ReadOps, e.Total.WriteOps
	}
	if m.direction == 3 {
		combinedPeak := 0.0
		start := m.history.Last.Add(-m.opts.Window)
		for _, s := range e.Segments {
			if s.End.After(start) {
				v := s.Rate.Read + s.Rate.Write
				if m.opts.Ops {
					v = s.Rate.ReadOps + s.Rate.WriteOps
				}
				combinedPeak = max(combinedPeak, v)
			}
		}
		line("SUM", now.Read+now.Write, a1.Read+a1.Write, aw.Read+aw.Write, combinedPeak, rtotal+wtotal)
		return
	}
	if m.direction == 0 || m.direction == 1 {
		line("READ", now.Read, a1.Read, aw.Read, peak.Read, rtotal)
	}
	if m.direction == 0 || m.direction == 2 {
		line("WRITE", now.Write, a1.Write, aw.Write, peak.Write, wtotal)
	}
}

type resolver struct {
	mu    sync.Mutex
	names map[string]string
	slots chan struct{}
	ctx   context.Context
}

func (r *resolver) label(e *Entity, group string) string {
	if group != "clients" && group != "servers" {
		return clean(e.Label)
	}
	r.mu.Lock()
	name, ok := r.names[e.Key]
	r.mu.Unlock()
	if ok {
		return name
	}
	select {
	case r.slots <- struct{}{}:
	default:
		return clean(e.Label)
	}
	r.mu.Lock()
	r.names[e.Key] = clean(e.Label)
	r.mu.Unlock()
	go func() {
		defer func() { <-r.slots }()
		ctx, cancel := context.WithTimeout(r.ctx, time.Second)
		defer cancel()
		names, err := net.DefaultResolver.LookupAddr(ctx, e.Key)
		if err == nil && len(names) > 0 {
			r.mu.Lock()
			r.names[e.Key] = clean(strings.TrimSuffix(names[0], "."))
			r.mu.Unlock()
		}
	}()
	return clean(e.Label)
}
func Run(name string, o Options, src collect.Source) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()
	if o.Duration > 0 {
		var stop context.CancelFunc
		ctx, stop = context.WithTimeout(ctx, o.Duration)
		defer stop()
	}
	start := time.Now()
	m := model{ctx: ctx, src: src, opts: o, history: NewHistory(start, o.Window), name: name, width: 110, height: 40,
		resolver: &resolver{names: map[string]string{}, slots: make(chan struct{}, 4), ctx: ctx}}
	if o.Text || os.Getenv("TERM") == "dumb" || !term.IsTerminal(int(os.Stdout.Fd())) || !term.IsTerminal(int(os.Stdin.Fd())) {
		return text(ctx, &m)
	}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
	final, err := p.Run() // Bubble Tea restores the primary screen, cursor, and original termios.
	if ctx.Err() != nil {
		return nil
	}
	if err != nil {
		return err
	}
	if f, ok := final.(model); ok {
		return f.err
	}
	return nil
}
func text(ctx context.Context, m *model) error {
	ticker := time.NewTicker(m.opts.Interval)
	defer ticker.Stop()
	encoder := json.NewEncoder(os.Stdout)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s, err := m.src.Read(ctx)
			if ctx.Err() != nil {
				return nil
			}
			if err != nil {
				return err
			}
			m.history.Add(s, m.opts)
			rows := m.history.Rows(m.opts.Sort)
			if m.opts.Limit > 0 && len(rows) > m.opts.Limit {
				rows = rows[:m.opts.Limit]
			}
			if m.opts.JSON {
				output := struct {
					Time                                          time.Time `json:"time"`
					Transport                                     string    `json:"transport"`
					WindowSeconds, ObservedSeconds, SampleSeconds float64
					Rows                                          []*Entity
					Total                                         *Entity
					Health                                        string
				}{Time: s.At, Transport: m.opts.Transport, WindowSeconds: m.opts.Window.Seconds(), ObservedSeconds: min(s.At.Sub(m.history.Start).Seconds(), m.opts.Window.Seconds()), SampleSeconds: m.opts.Interval.Seconds(), Rows: rows, Total: m.history.Total, Health: s.Health}
				// Segments stay private to the UI; public JSON carries summary metrics in bytes and bytes/s.
				summaries := make([]*Entity, 0, len(output.Rows))
				for _, e := range output.Rows {
					copy := *e
					copy.Segments = nil
					summaries = append(summaries, &copy)
				}
				output.Rows = summaries
				total := *output.Total
				total.Segments = nil
				output.Total = &total
				if err := encoder.Encode(output); err != nil {
					return err
				}
			} else {
				fmt.Fprintf(os.Stdout, "\n%s | %s NFS/%s | observed %.1fs | window %s\n", s.At.Format(time.RFC3339), m.name, m.opts.Transport, min(s.At.Sub(m.history.Start).Seconds(), m.opts.Window.Seconds()), windowLabel(m.opts.Window))
				fmt.Fprintf(os.Stdout, "%-35s %-5s %12s %12s %12s %12s %12s\n", "CLIENT / ENTITY", "DIR", "NOW", "AVG 1m", "AVG "+windowLabel(m.opts.Window), "PEAK "+windowLabel(m.opts.Window), "TOTAL")
				for _, e := range rows {
					m.entityBuilder(e)
				}
				m.entityBuilder(m.history.Total)
				if s.Health != "" {
					fmt.Fprintln(os.Stdout, "Health:", s.Health)
				}
			}
		}
	}
}
func (m model) entityBuilder(e *Entity) {
	var b strings.Builder
	m.entity(&b, e, 110)
	fmt.Print(b.String())
}

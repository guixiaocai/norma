// SPDX-License-Identifier: MIT
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
	"nfsmon/internal/collect"
)

type tick struct {
	snapshot collect.Snapshot
	err      error
	host     HostStats
}
type model struct {
	ctx                              context.Context
	src                              collect.Source
	opts                             Options
	history                          *History
	name                             string
	width, height, offset, direction int
	frozen                           *History
	paused                           bool
	err                              error
	resolver                         *resolver
	host                             *hostSampler
	hostStats                        HostStats
	frozenHost                       HostStats
	cursor                           int
	selectedKey                      string
	filtering                        bool
	diagnosticsView                  bool
	filterInput, filterError         string
	detailClient                     string
	navigation                       []viewState
}

type viewState struct {
	opts           Options
	detail         string
	cursor, offset int
	selected       string
}

func (m *model) pushView() {
	m.navigation = append(m.navigation, viewState{m.opts, m.detailClient, m.cursor, m.offset, m.selectedKey})
}
func (m *model) popView() bool {
	if len(m.navigation) == 0 {
		return false
	}
	v := m.navigation[len(m.navigation)-1]
	m.navigation = m.navigation[:len(m.navigation)-1]
	m.opts.Group, m.opts.Transport, m.opts.Client, m.opts.Filter, m.opts.FileFilter = v.opts.Group, v.opts.Transport, v.opts.Client, v.opts.Filter, v.opts.FileFilter
	m.detailClient, m.cursor, m.offset, m.selectedKey = v.detail, v.cursor, v.offset, v.selected
	return true
}

func (m model) next() tea.Cmd {
	return tea.Tick(m.opts.Interval, func(_ time.Time) tea.Msg {
		s, e := m.src.Read(m.ctx)
		var host HostStats
		if m.host != nil {
			host = m.host.Read(s.At)
		}
		return tick{snapshot: s, err: e, host: host}
	})
}
func (m model) Init() tea.Cmd { return m.next() }
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = v.Width, v.Height
	case tick:
		if v.err != nil {
			m.err = v.err
			return m, tea.Quit
		}
		m.history.Add(v.snapshot, m.opts)
		m.hostStats = v.host
		m.syncSelection(false)
		return m, m.next()
	case tea.KeyMsg:
		if v.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.filtering {
			switch v.String() {
			case "esc":
				m.filtering = false
				m.filterError = ""
			case "enter":
				if err := m.applyFilter(m.filterInput); err != nil {
					m.filterError = err.Error()
				} else {
					m.filtering = false
					m.reproject()
					m.cursor, m.offset = 0, 0
					m.selectedKey = ""
				}
			case "backspace", "ctrl+h":
				r := []rune(m.filterInput)
				if len(r) > 0 {
					m.filterInput = string(r[:len(r)-1])
				}
			case "ctrl+u":
				m.filterInput = ""
			default:
				if v.Type == tea.KeyRunes && len([]rune(m.filterInput)) < 256 {
					m.filterInput += string(v.Runes)
				}
			}
			m.syncSelection(false)
			return m, nil
		}
		selectionMoved := false
		switch v.String() {
		case "q":
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
		case "T":
			order := []string{"all", "tcp", "rdma", "udp"}
			for i, transport := range order {
				if transport == m.opts.Transport {
					m.opts.Transport = order[(i+1)%len(order)]
					break
				}
			}
			m.reproject()
			m.resetSelection()
		case "g":
			order := []string{"clients", "files", "users", "groups", "servers", "transports", "nodegroups"}
			for i, group := range order {
				if group == m.opts.Group {
					m.opts.Group = order[(i+1)%len(order)]
					break
				}
			}
			m.reproject()
			m.resetSelection()
		case "u":
			if m.detailClient != "" {
				if m.opts.Group == "users" {
					m.opts.Group = "files"
				} else {
					m.opts.Group = "users"
				}
				m.reproject()
				m.resetSelection()
			}
		case "d":
			m.diagnosticsView = !m.diagnosticsView
		case "/":
			m.filtering = true
			m.filterInput = ""
			m.filterError = ""
		case "enter":
			m.diagnosticsView = false
			rows := m.rows()
			if m.opts.Group == "transports" && m.cursor < len(rows) {
				transport := map[string]string{"6": "tcp", "17": "udp", "256": "rdma"}[rows[m.cursor].Key]
				if transport != "" {
					m.pushView()
					m.opts.Transport = transport
					m.opts.Group = "clients"
					m.reproject()
					m.resetSelection()
				}
			} else if m.opts.Group == "clients" && m.cursor < len(rows) {
				if addr, err := netip.ParseAddr(rows[m.cursor].Key); err == nil {
					m.pushView()
					m.detailClient = addr.String()
					prefix := netip.PrefixFrom(addr, addr.BitLen())
					m.opts.Client = &prefix
					m.opts.Group = "files"
					m.reproject()
					m.resetSelection()
				}
			}
		case "esc":
			if m.diagnosticsView {
				m.diagnosticsView = false
				break
			}
			if !m.popView() {
				m.opts.Client = nil
				m.opts.Filter = ""
				m.opts.FileFilter = ""
				m.detailClient = ""
				m.resetSelection()
			}
			m.reproject()
		case "p":
			if !m.paused {
				m.frozen = cloneHistory(m.history)
				m.frozenHost = m.hostStats
			}
			m.paused = !m.paused
		case "up", "k":
			m.cursor--
			selectionMoved = true
		case "down", "j":
			m.cursor++
			selectionMoved = true
		case "pgdown":
			page := max(1, m.pageSize())
			m.cursor += page
			m.offset += page
			selectionMoved = true
		case "pgup":
			page := max(1, m.pageSize())
			m.cursor -= page
			m.offset -= page
			selectionMoved = true
		}
		m.syncSelection(selectionMoved)
	}
	m.syncSelection(false)
	return m, nil
}
func (m *model) resetSelection() { m.cursor, m.offset = 0, 0; m.selectedKey = "" }
func (m *model) reproject() {
	m.history.Reproject(m.opts)
	if m.frozen != nil {
		m.frozen.Reproject(m.opts)
	}
}
func (m model) rows() []*Entity {
	history := m.history
	if m.paused && m.frozen != nil {
		history = m.frozen
	}
	return history.RowsSelected(m.opts.Sort, m.opts.Ops, m.direction)
}
func (m *model) syncSelection(moved bool) {
	rows := m.rows()
	if !moved && m.selectedKey != "" {
		for i, row := range rows {
			if row.Key == m.selectedKey {
				m.cursor = i
				break
			}
		}
	}
	m.cursor = max(0, min(m.cursor, len(rows)-1))
	page := max(1, m.pageSize())
	m.offset = max(0, min(m.offset, max(0, len(rows)-page)))
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+page {
		m.offset = m.cursor - page + 1
	}
	if len(rows) > 0 {
		m.selectedKey = rows[m.cursor].Key
	} else {
		m.selectedKey = ""
	}
}
func (m *model) applyFilter(query string) error {
	query = strings.TrimSpace(query)
	if query == "" {
		m.opts.Client = nil
		m.opts.Filter = ""
		m.opts.FileFilter = ""
		m.detailClient = ""
		return nil
	}
	ipOnly := strings.HasPrefix(query, "ip=")
	if ipOnly {
		query = strings.TrimPrefix(query, "ip=")
	}
	if addr, err := netip.ParseAddr(query); err == nil {
		addr = addr.Unmap()
		prefix := netip.PrefixFrom(addr, addr.BitLen())
		m.opts.Client = &prefix
		m.opts.Filter = query
		if m.detailClient != "" {
			m.detailClient = query
		}
		return nil
	}
	if prefix, err := netip.ParsePrefix(query); err == nil {
		prefix = prefix.Masked()
		m.opts.Client = &prefix
		m.opts.Filter = query
		if m.detailClient != "" {
			m.detailClient = query
		}
		return nil
	}
	if ipOnly {
		return fmt.Errorf("invalid IP/CIDR")
	}
	m.opts.FileFilter = strings.TrimPrefix(query, "file=")
	return nil
}
func (m model) View() string {
	if m.paused && m.frozen != nil {
		m.history = m.frozen
		m.hostStats = m.frozenHost
	}
	return m.render()
}
func windowLabel(d time.Duration) string {
	if d%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	return d.String()
}
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
func (m model) entity(b *strings.Builder, e *Entity, w int) {
	label := clean(e.Label)
	if !m.opts.Numeric && m.resolver != nil && !(m.opts.Group == "clients" && m.opts.Nodes[e.Key].Name != "") {
		label = m.resolver.label(e, m.opts.Group)
	}
	label = ansi.Truncate(label, max(18, w-75), "…")
	line := func(dir string, now, a1, aw, peak float64, total uint64) {
		format := func(metric string, v float64, perSecond bool) string {
			if !metricAvailable(e, metric) {
				return "—"
			}
			if m.opts.Ops {
				return humanOps(v, perSecond)
			}
			return human(v, m.opts.Bits, perSecond)
		}
		fmt.Fprintf(b, "%-*s %-5s %12s %12s %12s %12s %12s\n", max(18, w-75), label, dir, format("now", now, true), format("1m", a1, true), format("window", aw, true), format("peak", peak, true), format("total", float64(total), false))
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
			r.names[e.Key] = clean(strings.TrimSuffix(names[0], ".")) + " (" + clean(e.Key) + ")"
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
	if !o.Demo {
		m.host = newHostSampler()
	}
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
			if m.host != nil {
				m.hostStats = m.host.Read(s.At)
			}
			rows := m.rows()
			if m.opts.Limit > 0 && len(rows) > m.opts.Limit {
				rows = rows[:m.opts.Limit]
			}
			if m.opts.JSON {
				output := struct {
					Time time.Time `json:"time"
	"unicode"`
					Transport                                     string `json:"transport"`
					Demo                                          bool   `json:"demo,omitempty"`
					Group                                         string `json:"group"`
					ClientFilter                                  string `json:"client_filter,omitempty"`
					FileFilter                                    string `json:"file_filter,omitempty"`
					WindowSeconds, ObservedSeconds, SampleSeconds float64
					Rows                                          []*Entity
					Total                                         *Entity
					Health                                        string
					Host                                          HostStats
					Transports                                    map[uint32]Rate
				}{Time: s.At, Transport: m.opts.Transport, Demo: m.opts.Demo, Group: m.opts.Group, ClientFilter: m.opts.Filter, FileFilter: m.opts.FileFilter, WindowSeconds: m.opts.Window.Seconds(), ObservedSeconds: min(s.At.Sub(m.history.Start).Seconds(), m.opts.Window.Seconds()), SampleSeconds: m.opts.Interval.Seconds(), Rows: rows, Total: m.history.Total, Health: m.history.Health, Host: m.hostStats, Transports: m.history.Transports}
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
				if m.opts.Demo {
					fmt.Fprintln(os.Stdout, "DEMO — SYNTHETIC DATA (no kernel collection)")
				}
				fmt.Fprintf(os.Stdout, "\n%s | %s NFS/%s | observed %.1fs | window %s\n", s.At.Format(time.RFC3339), m.name, m.opts.Transport, min(s.At.Sub(m.history.Start).Seconds(), m.opts.Window.Seconds()), windowLabel(m.opts.Window))
				fmt.Fprintf(os.Stdout, "%-35s %-5s %12s %12s %12s %12s %12s\n", "CLIENT / ENTITY", "DIR", "NOW", "AVG 1m", "AVG "+windowLabel(m.opts.Window), "PEAK "+windowLabel(m.opts.Window), "TOTAL")
				for _, e := range rows {
					m.entityBuilder(e)
					seen := "—"
					if !e.FirstSeen.IsZero() {
						seen = e.FirstSeen.Format(time.RFC3339)
					}
					fmt.Fprintf(os.Stdout, "  first %s | avg/op READ %s WRITE %s | byte throughput share %.1f%%\n", seen, human(e.AverageReadSize, false, false), human(e.AverageWriteSize, false, false), e.Share)
					if e.Diagnostics != nil {
						fmt.Fprintln(os.Stdout, " ", latencyLine("READ", e.Diagnostics.Read, e.ReadLatency))
						fmt.Fprintln(os.Stdout, " ", latencyLine("WRITE", e.Diagnostics.Write, e.WriteLatency))
					}
				}
				m.entityBuilder(m.history.Total)
				if summary := m.hostStats.Summary(); summary != "" {
					fmt.Fprintln(os.Stdout, "Host:", summary)
				}
				if m.history.Health != "" {
					fmt.Fprintln(os.Stdout, "Health:", m.history.Health)
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

// SPDX-License-Identifier: MIT
package app

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"nfsmon/internal/collect"
)

func dashboardFixture() model {
	start := time.Date(2026, 10, 15, 14, 32, 0, 0, time.UTC)
	opts := Options{Backend: "native", Transport: "all", Group: "clients", Sort: "now", Interval: time.Second, Window: 5 * time.Minute, Numeric: true, Demo: true}
	source := newDemoSource()
	source.start, source.last = start, start
	history := NewHistory(start, opts.Window)
	for i := 1; i <= 120; i++ {
		history.Add(source.sample(start.Add(time.Duration(i)*time.Second)), opts)
	}
	return model{name: "norma", opts: opts, history: history, src: source, width: 140, height: 40}
}

func assertFrameFits(t *testing.T, frame string, width, height int) {
	t.Helper()
	lines := strings.Split(frame, "\n")
	if len(lines) > height {
		t.Fatalf("frame has %d rows in a %d-row terminal", len(lines), height)
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got > width {
			t.Errorf("line %d is %d cells wide in a %d-column terminal: %q", i, got, width, ansi.Strip(line))
		}
	}
}

func TestDashboardResponsiveBounds(t *testing.T) {
	for _, size := range [][2]int{{140, 40}, {120, 28}, {100, 30}, {80, 28}, {80, 24}, {60, 20}, {44, 14}, {32, 12}, {10, 6}, {1, 1}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := dashboardFixture()
			m.width, m.height = size[0], size[1]
			for _, e := range m.history.Entities {
				e.Label = "研究数据/顕微鏡/very-long-dataset-👩‍🔬-name.bin"
			}
			for direction := 0; direction < 4; direction++ {
				m.direction = direction
				assertFrameFits(t, m.View(), m.width, m.height)
			}
		})
	}
}

func TestDashboardColumnsAdapt(t *testing.T) {
	for _, tc := range []struct {
		width, count int
		want         string
	}{{140, 5, "PEAK 5m"}, {100, 4, "AVG 1m"}, {80, 3, "AVG 5m"}, {60, 2, "SESSION"}, {44, 1, "NOW"}} {
		m := dashboardFixture()
		m.width, m.height = tc.width, 30
		if got := len(m.columns(tc.width)); got != tc.count {
			t.Fatalf("width %d: %d metrics, want %d", tc.width, got, tc.count)
		}
		if !strings.Contains(ansi.Strip(m.View()), tc.want) {
			t.Errorf("width %d is missing %s", tc.width, tc.want)
		}
	}
}

func TestDashboardPauseKeepsSamplingAndResizes(t *testing.T) {
	m := dashboardFixture()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(model)
	frozenRead, frozenLast := m.frozen.Total.Total.Read, m.frozen.Last
	snapshot := m.src.(*demoSource).sample(m.history.Last.Add(time.Second))
	updated, next := m.Update(tick{snapshot: snapshot})
	m = updated.(model)
	if next == nil || m.history.Last.Equal(frozenLast) || m.history.Total.Total.Read <= frozenRead {
		t.Fatal("sampling stopped while the display was paused")
	}
	if m.frozen.Total.Total.Read != frozenRead || !m.frozen.Last.Equal(frozenLast) {
		t.Fatal("paused history changed when live samples arrived")
	}
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 60, Height: 18})
	m = updated.(model)
	frame := m.View()
	assertFrameFits(t, frame, 60, 18)
	if !strings.Contains(ansi.Strip(frame), "PAUSED") || !strings.Contains(ansi.Strip(frame), "DEMO") {
		t.Fatal("paused demo lost its status after resizing")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(model)
	if m.paused || strings.Contains(ansi.Strip(m.View()), "PAUSED") {
		t.Fatal("resume did not restore live display")
	}
}

func TestDashboardHealthAndScrolling(t *testing.T) {
	m := dashboardFixture()
	m.opts.Demo = false
	m.width, m.height = 80, 28
	m.history.Health = "read_errors=1 file_drops=2 client_drops=3 inflight_drops=4 missed_or_overwritten=5 failed_ops=6 name_hint_drops=7"
	frame := ansi.Strip(m.View())
	for _, want := range []string{"ATTENTION", "read_errors=1", "name_hint_drops=7", "quit"} {
		if !strings.Contains(frame, want) {
			t.Errorf("health frame lost %q", want)
		}
	}
	assertFrameFits(t, m.View(), 80, 28)
	m.offset = 10000
	frame = ansi.Strip(m.View())
	if !strings.Contains(frame, "–8 of 8") || !strings.Contains(frame, m.history.Rows(m.opts.Sort)[7].Label) {
		t.Fatalf("scroll did not clamp to final entities:\n%s", frame)
	}
}

func TestDashboardScrollingUsesVisiblePage(t *testing.T) {
	m := dashboardFixture()
	m.width, m.height = 80, 28
	page := m.pageSize()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = updated.(model)
	if m.offset != page {
		t.Fatalf("page down moved %d entities, want visible page %d", m.offset, page)
	}
	for i := 0; i < 100; i++ {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(model)
	}
	last := len(m.history.Entities) - page
	if m.offset != last {
		t.Fatalf("scroll offset %d was not clamped to %d", m.offset, last)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(model)
	if m.offset != last-1 {
		t.Fatal("scrolling up remained stuck beyond the last visible page")
	}
}

func TestDashboardNoColor(t *testing.T) {
	for _, viaEnvironment := range []bool{false, true} {
		m := dashboardFixture()
		t.Setenv("NO_COLOR", "")
		if viaEnvironment {
			t.Setenv("NO_COLOR", "1")
		} else {
			m.opts.NoColor = true
		}
		if strings.Contains(m.View(), "\x1b") {
			t.Fatal("no-color output contains an escape sequence")
		}
	}
}

func TestDashboardCombinedPeakIsSimultaneous(t *testing.T) {
	m := dashboardFixture()
	m.direction = 3
	m.opts.Demo = false
	start := m.history.Start
	m.history = NewHistory(start, m.opts.Window)
	m.history.Entities["test"] = &Entity{Key: "test", Label: "test", Total: collect.Counters{Read: 100, Write: 100},
		Now: Rate{Write: 100}, Average1: Rate{Read: 50, Write: 50}, AverageW: Rate{Read: 50, Write: 50}, PeakW: Rate{Read: 100, Write: 100},
		Segments: []Segment{{Start: start, End: start.Add(time.Second), Rate: Rate{Read: 100}}, {Start: start.Add(time.Second), End: start.Add(2 * time.Second), Rate: Rate{Write: 100}}}}
	m.history.Last = start.Add(2 * time.Second)
	frame := ansi.Strip(m.View())
	if strings.Contains(frame, "200B/s") || !strings.Contains(frame, "100B/s") {
		t.Fatalf("combined peak should be the maximum simultaneous rate:\n%s", frame)
	}
}

// Produce documentation assets from the same View used by the running TUI.
// Example: NORMA_SCREENSHOT=/tmp/norma.ansi go test ./internal/app -run TestDashboardScreenshot
func TestDashboardScreenshot(t *testing.T) {
	path := os.Getenv("NORMA_SCREENSHOT")
	if path == "" {
		t.Skip("set NORMA_SCREENSHOT to export the actual demo dashboard frame")
	}
	t.Setenv("NO_COLOR", "")
	m := dashboardFixture()
	frame := m.View()
	assertFrameFits(t, frame, m.width, m.height)
	if err := os.WriteFile(path, []byte(frame), 0644); err != nil {
		t.Fatal(err)
	}
}

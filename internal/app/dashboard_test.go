// SPDX-License-Identifier: MIT
package app

import (
	"fmt"
	"net/netip"
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
	if m.cursor != len(m.history.Entities)-2 {
		t.Fatal("selection remained stuck beyond the last visible row")
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

// screenshotDetailFixture changes synthetic workload inputs only: each sampled
// client's successful deltas are distributed across three files and UIDs. The
// real History still computes every displayed rate, total and grouping, while
// the independent client summaries and latency diagnostics remain unchanged.
func screenshotDetailFixture() model {
	m := dashboardFixture()
	source := newDemoSource()
	source.start, source.last = m.history.Start, m.history.Start
	m.history = NewHistory(source.start, m.opts.Window)
	m.src = source
	previous := make([]collect.Counters, len(source.records))
	cumulative := make([][3]collect.Counters, len(source.records))
	share := func(value uint64, first, second uint64) [3]uint64 {
		// Divide each nonnegative sample delta, then accumulate the shares.
		// Dividing cumulative values and putting rounding in the final file can
		// make that file's counter decrease at rounding boundaries.
		a := value/10*first + value%10*first/10
		b := value/10*second + value%10*second/10
		return [3]uint64{a, b, value - a - b}
	}
	for sample := 1; sample <= 120; sample++ {
		snapshot := source.sample(source.start.Add(time.Duration(sample) * time.Second))
		original := snapshot.Records
		snapshot.Records = make([]collect.Record, 0, len(original)*3)
		for i, record := range original {
			delta := record.Counters.Delta(previous[i])
			previous[i] = record.Counters
			reads, writes := share(delta.Read, 6, 3), share(delta.Write, 1, 6)
			readOps, writeOps := share(delta.ReadOps, 6, 3), share(delta.WriteOps, 1, 6)
			names := []string{fmt.Sprintf("datasets/batch-%02d.bin", i+1), fmt.Sprintf("results/summary-%02d.dat", i+1), fmt.Sprintf("checkpoints/task-%02d.ckpt", i+1)}
			for part := 0; part < 3; part++ {
				detail := record
				detail.Inode = record.Inode*3 + uint64(part)
				detail.UID = uint32(1000 + part)
				detail.Name = names[part]
				cumulative[i][part] = cumulative[i][part].Add(collect.Counters{Read: reads[part], Write: writes[part], ReadOps: readOps[part], WriteOps: writeOps[part]})
				detail.Counters = cumulative[i][part]
				snapshot.Records = append(snapshot.Records, detail)
			}
		}
		m.history.Add(snapshot, m.opts)
	}
	return m
}

func screenshotModel(view string) (model, error) {
	if view == "" {
		view = "overview"
	}
	var m model
	switch view {
	case "overview", "transports", "diagnostics":
		m = dashboardFixture()
	case "files", "users":
		m = screenshotDetailFixture()
	default:
		return model{}, fmt.Errorf("unknown NORMA_SCREENSHOT_VIEW %q; choose overview, files, users, transports or diagnostics", view)
	}
	switch view {
	case "files", "users":
		m = keyUpdate(m, "enter")
		if view == "users" {
			m = keyUpdate(m, "u")
		}
	case "transports":
		for step := 0; step < 7 && m.opts.Group != "transports"; step++ {
			m = keyUpdate(m, "g")
		}
	case "diagnostics":
		// The third ranked demo client has synthetic failures, so this view
		// demonstrates error counts as well as successful helper timings.
		m = keyUpdate(m, "down")
		m = keyUpdate(m, "down")
		updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		m = keyUpdate(updated.(model), "d")
	}
	return m, nil
}

// Produce documentation assets from the same View/Update used by the running
// TUI. NORMA_SCREENSHOT_VIEW defaults to overview; other supported views are
// files, users, transports and diagnostics. Every dataset is explicitly DEMO.
// Example:
// NORMA_SCREENSHOT=/tmp/files.ansi NORMA_SCREENSHOT_VIEW=files go test ./internal/app -run '^TestDashboardScreenshot$' -count=1
func TestDashboardScreenshot(t *testing.T) {
	path := os.Getenv("NORMA_SCREENSHOT")
	if path == "" {
		t.Skip("set NORMA_SCREENSHOT to export the actual demo dashboard frame")
	}
	t.Setenv("NO_COLOR", "")
	m, err := screenshotModel(os.Getenv("NORMA_SCREENSHOT_VIEW"))
	if err != nil {
		t.Fatal(err)
	}
	frame := m.View()
	assertFrameFits(t, frame, m.width, m.height)
	if !strings.Contains(ansi.Strip(frame), "DEMO") {
		t.Fatal("synthetic screenshot lost its DEMO label")
	}
	if err := os.WriteFile(path, []byte(frame), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestDashboardScreenshotVariants(t *testing.T) {
	for _, tc := range []struct {
		view, group string
		entities    int
	}{
		{"overview", "clients", 8}, {"files", "files", 3}, {"users", "users", 3}, {"transports", "transports", 3}, {"diagnostics", "clients", 8},
	} {
		t.Run(tc.view, func(t *testing.T) {
			m, err := screenshotModel(tc.view)
			if err != nil {
				t.Fatal(err)
			}
			if m.opts.Group != tc.group || len(m.history.Entities) != tc.entities {
				t.Fatalf("wrong screenshot population: group=%s entities=%d", m.opts.Group, len(m.history.Entities))
			}
			frame := m.View()
			assertFrameFits(t, frame, m.width, m.height)
			if !strings.Contains(ansi.Strip(frame), "DEMO") {
				t.Fatal("synthetic screenshot is not labelled")
			}
			if tc.view == "files" || tc.view == "users" {
				if len(m.navigation) != 1 || m.detailClient == "" || m.opts.Client == nil {
					t.Fatal("screenshot bypassed client drill-down")
				}
			}
			if tc.view == "diagnostics" && (!m.diagnosticsView || m.width != 80 || m.height != 24 || m.selectedEntity().Diagnostics.Read.Errors == 0) {
				t.Fatal("diagnostics screenshot lacks selected synthetic failures or compact statistics view")
			}
		})
	}
	if _, err := screenshotModel("unknown"); err == nil {
		t.Fatal("unsupported screenshot view was accepted")
	}
}

func TestScreenshotDetailFixtureConservesClientCounters(t *testing.T) {
	m := screenshotDetailFixture()
	for key, client := range m.history.Entities {
		address := netip.MustParseAddr(key)
		prefix := netip.PrefixFrom(address, address.BitLen())
		opts := m.opts
		opts.Group, opts.Client = "files", &prefix
		files := m.history.Project(opts)
		if len(files.Entities) != 3 || files.Total.Total != client.Total || files.Total.Now != client.Now {
			t.Fatalf("detail fixture changed endpoint accounting for %s", key)
		}
		if !files.Total.Average1Valid || !files.Total.AverageWValid || !files.Total.PeakWValid {
			t.Fatal("synthetic detail history unexpectedly unavailable")
		}
		opts.Group = "users"
		users := m.history.Project(opts)
		if len(users.Entities) != 3 || users.Total.Total != client.Total {
			t.Fatalf("detail fixture lost UID attribution for %s", key)
		}
	}
	baseline := dashboardFixture()
	if m.history.Total.Total != baseline.history.Total.Total || m.history.Total.Now != baseline.history.Total.Now {
		t.Fatal("screenshot fixture changed overview counters")
	}
}

func keyUpdate(m model, key string) model {
	var event tea.KeyMsg
	switch key {
	case "enter":
		event = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		event = tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		event = tea.KeyMsg{Type: tea.KeyDown}
	default:
		event = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	updated, _ := m.Update(event)
	return updated.(model)
}
func TestDashboardRuntimeFilterAndGrouping(t *testing.T) {
	m := dashboardFixture()
	total := m.history.Total.Total
	m = keyUpdate(m, "/")
	m = keyUpdate(m, "file=dataset-01")
	if !m.filtering || !strings.Contains(m.filterInput, "dataset-01") {
		t.Fatal("filter prompt did not collect text")
	}
	m = keyUpdate(m, "enter")
	if m.filtering || m.opts.FileFilter != "dataset-01" || len(m.history.Entities) != 1 {
		t.Fatal("filename filter did not update selected population")
	}
	m = keyUpdate(m, "g")
	if m.opts.Group != "files" || len(m.history.Entities) != 1 {
		t.Fatal("runtime grouping lost filter")
	}
	m = keyUpdate(m, "esc")
	if m.opts.FileFilter != "" || m.history.Total.Total != total {
		t.Fatal("clearing filter lost previously collected session counters")
	}
	m = keyUpdate(m, "/")
	m = keyUpdate(m, "ip=invalid")
	m = keyUpdate(m, "enter")
	if !m.filtering || !strings.Contains(m.filterError, "invalid") {
		t.Fatal("explicit malformed IP filter was accepted")
	}
	m = keyUpdate(m, "esc")
	if m.filtering {
		t.Fatal("escape did not cancel filter entry")
	}
}
func TestDashboardDrillDownAndReturnStack(t *testing.T) {
	m := dashboardFixture()
	m.opts.Group = "transports"
	m.reproject()
	m.syncSelection(false)
	m = keyUpdate(m, "enter")
	if m.opts.Group != "clients" || m.opts.Transport == "all" {
		t.Fatal("transport Enter did not expand its clients")
	}
	selected := m.rows()[0].Key
	session := m.history.Total.Total
	protocol := m.opts.Transport
	m = keyUpdate(m, "enter")
	if m.opts.Group != "files" || m.detailClient != selected || m.opts.Client == nil {
		t.Fatal("Enter did not select the exact client")
	}
	m = keyUpdate(m, "u")
	if m.opts.Group != "users" {
		t.Fatal("UID detail toggle missing")
	}
	m = keyUpdate(m, "esc")
	if m.opts.Group != "clients" || m.detailClient != "" || m.history.Total.Total != session || m.opts.Transport != protocol {
		t.Fatal("return lost parent population or session")
	}
	m = keyUpdate(m, "esc")
	if m.opts.Group != "transports" || m.opts.Transport != "all" {
		t.Fatal("second return lost original transport grouping")
	}
}
func TestDashboardPausedRegroupUsesFrozenRawHistory(t *testing.T) {
	m := dashboardFixture()
	m = keyUpdate(m, "p")
	frozen := m.frozen.Total.Total.Read
	snapshot := m.src.(*demoSource).sample(m.history.Last.Add(time.Second))
	updated, _ := m.Update(tick{snapshot: snapshot})
	m = updated.(model)
	m = keyUpdate(m, "g")
	if m.opts.Group != "files" || m.frozen.Total.Total.Read != frozen || m.history.Total.Total.Read <= frozen {
		t.Fatal("paused regroup read live retained records")
	}
	assertFrameFits(t, m.View(), m.width, m.height)
}
func TestDashboardSmallScreenStatisticsView(t *testing.T) {
	m := dashboardFixture()
	m.width, m.height = 80, 24
	m = keyUpdate(m, "d")
	if !m.diagnosticsView || !strings.Contains(ansi.Strip(m.View()), "SESSION STATISTICS") {
		t.Fatal("statistics inaccessible on an 80-column terminal")
	}
	assertFrameFits(t, m.View(), 80, 24)
	m = keyUpdate(m, "esc")
	if m.diagnosticsView {
		t.Fatal("Escape did not return from statistics")
	}
}
func TestDashboardUnavailableHistoryUsesDash(t *testing.T) {
	m := dashboardFixture()
	m.opts.NoColor = true
	for _, e := range m.history.Entities {
		e.Average1Valid, e.AverageWValid, e.PeakWValid = false, false, false
	}
	m.history.Total.Average1Valid = false
	frame := m.View()
	if !strings.Contains(frame, "—") {
		t.Fatal("unavailable history displayed as a numeric zero")
	}
}

func TestCleanTerminalControlsPreservesUnicode(t *testing.T) {
	input := "文件👩‍🔬\x00\t\n\x1b\x7f\u0085\u009b\u009f"
	want := "文件👩‍🔬" + strings.Repeat(" ", 8)
	if got := clean(input); got != want {
		t.Fatalf("terminal control sanitation: got %q, want %q", got, want)
	}
}

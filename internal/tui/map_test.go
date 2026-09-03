package tui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Ashes47/braids/internal/core/graph"
	"github.com/Ashes47/braids/internal/core/index"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plain(s string) string { return ansi.ReplaceAllString(s, "") }

var now = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func laneInfo(id, title, project string, msgs int, age time.Duration) index.LaneInfo {
	li := index.LaneInfo{Messages: msgs}
	li.ID = id
	li.Title = title
	li.Project = project
	li.Updated = now.Add(-age)
	return li
}

// forestOf builds a forest with explicit parent links, bypassing detection.
func forestOf(lanes []index.LaneInfo, parents map[string]string) *graph.Forest {
	f := &graph.Forest{ByID: map[string]*graph.Node{}}
	for _, l := range lanes {
		f.ByID[l.ID] = &graph.Node{Lane: l}
	}
	for child, parent := range parents {
		f.ByID[child].ParentID = parent
		f.ByID[child].ForkSeq = 12
	}
	// Iterate the input slice, not the map: map order is random, and a random
	// row order makes any assertion about a specific row flaky.
	for _, l := range lanes {
		n := f.ByID[l.ID]
		if p, ok := f.ByID[n.ParentID]; ok {
			p.Children = append(p.Children, n)
			n.Depth = 1
			continue
		}
		f.Roots = append(f.Roots, n)
	}
	return f
}

// rowFor returns the rendered line showing the given title.
func rowFor(t *testing.T, out, title string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, title) {
			return line
		}
	}
	t.Fatalf("no row for %q in:\n%s", title, out)
	return ""
}

func newTestModel(t *testing.T, f *graph.Forest) Model {
	t.Helper()
	// ASCII glyphs keep assertions width-stable.
	m := NewModel(f, Options{ASCII: true, Source: "claudecode", IndexPath: "/tmp/index.db"})
	m.now = func() time.Time { return now }
	m.width, m.height = 90, 20
	return m
}

func TestLayoutDrawsTreeConnectors(t *testing.T) {
	lanes := []index.LaneInfo{
		laneInfo("root", "main work", "app", 100, time.Hour),
		laneInfo("a", "first branch", "app", 10, time.Hour),
		laneInfo("b", "second branch", "app", 10, 2*time.Hour),
	}
	f := forestOf(lanes, map[string]string{"a": "root", "b": "root"})
	rows := layout(f, glyphsFor(true))

	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(rows))
	}
	if rows[0].prefix != "" {
		t.Errorf("root prefix = %q, want empty", rows[0].prefix)
	}
	prefixes := []string{rows[1].prefix, rows[2].prefix}
	if prefixes[0] != "|-" || prefixes[1] != "`-" {
		t.Errorf("child prefixes = %v, want a branch then a last-child connector", prefixes)
	}
}

func TestRenderShowsLanesAndChrome(t *testing.T) {
	lanes := []index.LaneInfo{
		laneInfo("root", "main work", "app", 100, 2*time.Minute),
		laneInfo("kid", "a branch", "app", 8, 30*time.Hour),
	}
	m := newTestModel(t, forestOf(lanes, map[string]string{"kid": "root"}))
	out := plain(m.render())

	for _, want := range []string{
		"Source:", "claudecode", "Lanes:", "Active:",
		"Conversations(all)[2]", "CONVERSATION", "STATUS",
		"main work", "a branch", "<j/k>", "active", "idle",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "< t12") {
		t.Errorf("fork column should appear when a fork exists:\n%s", out)
	}
	if !strings.Contains(out, "1d") {
		t.Errorf("expected an age of 1d for the branch:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if w := len([]rune(line)); w > m.width {
			t.Errorf("line exceeds width %d (%d): %q", m.width, w, line)
		}
	}
}

func TestOptionalColumnsAppearOnlyWhenUseful(t *testing.T) {
	t.Run("no forks means no fork column", func(t *testing.T) {
		lanes := []index.LaneInfo{laneInfo("a", "one", "app", 5, time.Hour), laneInfo("b", "two", "app", 5, time.Hour)}
		m := newTestModel(t, forestOf(lanes, nil))
		if m.showFork {
			t.Error("showFork should be false with no forks")
		}
		out := plain(m.render())
		if strings.Contains(out, "FORK") || strings.Contains(out, "< t") {
			t.Errorf("fork column rendered without any fork:\n%s", out)
		}
	})

	t.Run("one project means no project column", func(t *testing.T) {
		lanes := []index.LaneInfo{laneInfo("a", "one", "app", 5, time.Hour)}
		if newTestModel(t, forestOf(lanes, nil)).showProject {
			t.Error("showProject should be false for a single project")
		}
	})

	t.Run("several projects earn the column", func(t *testing.T) {
		lanes := []index.LaneInfo{
			laneInfo("a", "one", "app", 5, time.Hour),
			laneInfo("b", "two", "infra", 5, time.Hour),
		}
		m := newTestModel(t, forestOf(lanes, nil))
		if !m.showProject {
			t.Fatal("showProject should be true for two projects")
		}
		if !strings.Contains(plain(m.render()), "infra") {
			t.Error("project column missing")
		}
	})
}

func TestDuplicateTitlesGetTheirLaneID(t *testing.T) {
	lanes := []index.LaneInfo{
		laneInfo("9419fd9cxxxx", "Debug pipeline", "app", 100, time.Hour),
		laneInfo("106997f5yyyy", "Debug pipeline", "app", 50, time.Hour),
		laneInfo("uniqueaaaa", "Something else", "app", 5, time.Hour),
	}
	m := newTestModel(t, forestOf(lanes, nil))
	out := plain(m.render())

	// Assert per row: the status line names the selected lane's full ID, so a
	// whole-frame search would see IDs that no row is actually showing.
	first := rowFor(t, out, "Debug pipeline")
	if !strings.Contains(first, "9419fd9c") {
		t.Errorf("ambiguous title must show its lane ID: %q", first)
	}
	unique := rowFor(t, out, "Something else")
	if strings.Contains(unique, "uniqueaa") {
		t.Errorf("unambiguous title should not be cluttered with an ID: %q", unique)
	}
}

func TestFilterNarrowsAndClears(t *testing.T) {
	lanes := []index.LaneInfo{
		laneInfo("a", "gcsfuse density", "app", 5, time.Hour),
		laneInfo("b", "schema refactor", "app", 5, time.Hour),
	}
	m := newTestModel(t, forestOf(lanes, nil))

	press := func(m Model, keys ...string) Model {
		for _, k := range keys {
			updated, _ := m.Update(tea.KeyPressMsg{Code: rune(k[0]), Text: k})
			m = updated.(Model)
		}
		return m
	}

	m = press(m, "/", "s", "c", "h")
	if len(m.visible) != 1 || m.visible[0].node.Lane.ID != "b" {
		t.Fatalf("filter left %d lanes, want just the schema one", len(m.visible))
	}
	if out := plain(m.render()); !strings.Contains(out, "Conversations(/sch)[1]") {
		t.Errorf("panel title should show the filter and count:\n%s", out)
	}

	esc, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = esc.(Model)
	if len(m.visible) != 2 {
		t.Errorf("escape should clear the filter, got %d lanes", len(m.visible))
	}
}

func TestFilterWithNoMatchesExplainsItself(t *testing.T) {
	lanes := []index.LaneInfo{laneInfo("a", "gcsfuse", "app", 5, time.Hour)}
	m := newTestModel(t, forestOf(lanes, nil))
	m.filter.text = "zzz"
	m.apply()
	if out := plain(m.render()); !strings.Contains(out, `nothing matches "zzz"`) {
		t.Errorf("expected an empty-state message:\n%s", out)
	}
}

func TestCursorStaysInRange(t *testing.T) {
	var lanes []index.LaneInfo
	for i := range 5 {
		lanes = append(lanes, laneInfo(string(rune('a'+i)), "lane", "app", 1, time.Hour))
	}
	m := newTestModel(t, forestOf(lanes, nil))
	m.height = chromeHeight + 2 // room for two rows

	for range 20 {
		m.cursor++
		m.clamp()
	}
	if m.cursor != len(m.visible)-1 {
		t.Errorf("cursor = %d, want clamped to %d", m.cursor, len(m.visible)-1)
	}
	if m.cursor < m.offset || m.cursor >= m.offset+m.bodyHeight() {
		t.Errorf("cursor %d outside the visible window [%d,%d)", m.cursor, m.offset, m.offset+m.bodyHeight())
	}
	for range 20 {
		m.cursor--
		m.clamp()
	}
	if m.cursor != 0 || m.offset != 0 {
		t.Errorf("cursor/offset = %d/%d, want 0/0", m.cursor, m.offset)
	}
}

func TestTruncateMeasuresDisplayWidth(t *testing.T) {
	tests := []struct {
		in, want string
		w        int
	}{
		{"short", "short", 10},
		{"truncate me here", "truncate…", 9},
		{"", "", 5},
		{"日本語テキスト", "日本…", 6}, // wide runes must not overflow the column
	}
	for _, tt := range tests {
		got := truncate(tt.in, tt.w)
		if got != tt.want {
			t.Errorf("truncate(%q,%d) = %q, want %q", tt.in, tt.w, got, tt.want)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{512, "512 B"},
		{2048, "2 kB"},
		{5 << 20, "5 MB"},
		{3 << 30, "3.0 GB"},
	}
	for _, tt := range tests {
		if got := humanBytes(tt.n); got != tt.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestShortenReplacesHome(t *testing.T) {
	orig := homeDir
	homeDir = func() (string, error) { return "/Users/x", nil }
	defer func() { homeDir = orig }()

	if got := shorten("/Users/x/.braids/index.db"); got != "~/.braids/index.db" {
		t.Errorf("shorten = %q", got)
	}
	if got := shorten("/etc/other"); got != "/etc/other" {
		t.Errorf("shorten should leave unrelated paths alone, got %q", got)
	}
}

func TestStatusLineNamesTheSelectedLane(t *testing.T) {
	lanes := []index.LaneInfo{laneInfo("abc123", "one", "app", 5, time.Hour)}
	m := newTestModel(t, forestOf(lanes, nil))
	if !strings.Contains(plain(m.render()), "abc123") {
		t.Error("status line should name the selected lane")
	}
}

func TestHumanAge(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "now"},
		{5 * time.Minute, "5m"},
		{3 * time.Hour, "3h"},
		{50 * time.Hour, "2d"},
	}
	for _, tt := range tests {
		if got := humanAge(tt.d); got != tt.want {
			t.Errorf("humanAge(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

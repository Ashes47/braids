package tui

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Ashes47/braids/internal/brand"
	"github.com/Ashes47/braids/internal/core/graph"
	"github.com/Ashes47/braids/internal/core/index"
	"github.com/Ashes47/braids/internal/core/model"
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
	rows := layout(f, glyphsFor(true), nil)

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
		"Source:", "claudecode", "Lanes:", "Waiting on you:",
		"Conversations(all)[2]", "CONVERSATION", "STATUS",
		"main work", "a branch", "<j/k>",
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

	// f opens the filter; / is global search everywhere in braids.
	m = press(m, "f", "s", "c", "h")
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
	m.height = m.chromeHeight() + 2 // room for two rows

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

func TestLiveUpdatesAdoptANewForest(t *testing.T) {
	before := []index.LaneInfo{laneInfo("a", "first", "app", 5, time.Hour)}
	after := []index.LaneInfo{
		laneInfo("a", "first", "app", 5, time.Hour),
		laneInfo("b", "appeared while watching", "app", 2, time.Minute),
	}
	changes := make(chan struct{}, 1)
	refreshed := 0

	m := NewModel(forestOf(before, nil), Options{
		ASCII:   true,
		Source:  "claudecode",
		Changes: changes,
		Refresh: func() (*graph.Forest, error) {
			refreshed++
			return forestOf(after, nil), nil
		},
	})
	m.now = func() time.Time { return now }
	m.width, m.height = 90, 20

	if !strings.Contains(plain(m.render()), "· live") {
		t.Error("a watching map should say so")
	}

	// A file change must not read the index on this thread: a 145 MB
	// conversation would freeze the frame for as long as it took.
	updated, cmd := m.Update(changedMsg{})
	m = updated.(Model)
	if refreshed != 0 {
		t.Fatalf("refresh ran on the interface thread %d times", refreshed)
	}
	if cmd == nil {
		t.Fatal("the map must re-arm its listener or it goes deaf after one change")
	}
	if !m.refreshing {
		t.Error("the map does not know a read is in flight")
	}

	// The read happens, and its result is adopted when it arrives.
	msg := refreshInBackground(m)()
	if refreshed != 1 {
		t.Fatalf("refresh called %d times, want 1", refreshed)
	}
	updated, _ = m.Update(msg)
	m = updated.(Model)
	if m.refreshing {
		t.Error("the map still thinks a read is in flight")
	}
	if !strings.Contains(plain(m.render()), "appeared while watching") {
		t.Error("the new lane should be on the map")
	}
}

// Changes arriving while a read is in flight are coalesced: one more read
// after it finishes, not one per change.
func TestLiveUpdatesCoalesceWhileReading(t *testing.T) {
	lanes := []index.LaneInfo{laneInfo("a", "first", "app", 5, time.Hour)}
	changes := make(chan struct{}, 1)
	refreshed := 0
	m := NewModel(forestOf(lanes, nil), Options{
		ASCII: true, Source: "claudecode", Changes: changes,
		Refresh: func() (*graph.Forest, error) {
			refreshed++
			return forestOf(lanes, nil), nil
		},
	})
	m.now = func() time.Time { return now }
	m.width, m.height = 90, 20

	for range 4 {
		updated, _ := m.Update(changedMsg{})
		m = updated.(Model)
	}
	if !m.refreshing || !m.refreshAgain {
		t.Fatalf("refreshing=%v again=%v, want one in flight and more noted",
			m.refreshing, m.refreshAgain)
	}
	// Finishing the first read starts exactly one more.
	updated, cmd := m.Update(refreshInBackground(m)())
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("the noted changes were dropped")
	}
	if m.refreshAgain {
		t.Error("the note was not cleared, so reads would never stop")
	}
	// And that one settles.
	updated, cmd = m.Update(refreshInBackground(m)())
	m = updated.(Model)
	if m.refreshing || cmd != nil {
		t.Errorf("reads did not settle: refreshing=%v cmd=%v", m.refreshing, cmd != nil)
	}
	if refreshed != 2 {
		t.Errorf("read the index %d times for four changes, want 2", refreshed)
	}
}

func TestLiveUpdateKeepsThePlace(t *testing.T) {
	lanes := []index.LaneInfo{
		laneInfo("a", "first", "app", 5, time.Hour),
		laneInfo("b", "second", "app", 5, time.Hour),
	}
	m := NewModel(forestOf(lanes, nil), Options{
		ASCII:   true,
		Changes: make(chan struct{}),
		Refresh: func() (*graph.Forest, error) { return forestOf(lanes, nil), nil },
	})
	m.now = func() time.Time { return now }
	m.width, m.height = 90, 20
	m.cursor = 1
	selected := m.visible[1].node.Lane.ID

	updated, _ := m.Update(changedMsg{})
	m = updated.(Model)
	if got := m.visible[m.cursor].node.Lane.ID; got != selected {
		t.Errorf("cursor moved to %q across a refresh, want %q", got, selected)
	}
}

func TestRefreshFailureIsSurvivable(t *testing.T) {
	lanes := []index.LaneInfo{laneInfo("a", "first", "app", 5, time.Hour)}
	m := NewModel(forestOf(lanes, nil), Options{
		ASCII:   true,
		Changes: make(chan struct{}),
		Refresh: func() (*graph.Forest, error) { return nil, errors.New("half-written file") },
	})
	m.now = func() time.Time { return now }
	m.width, m.height = 90, 20

	updated, cmd := m.Update(changedMsg{})
	m = updated.(Model)
	if cmd == nil {
		t.Error("a failed refresh must still re-arm; transcripts settle on the next write")
	}
	if !strings.Contains(plain(m.render()), "first") {
		t.Error("the map should keep showing what it had")
	}
}

func TestSnapshotMapSaysNothingAboutLive(t *testing.T) {
	lanes := []index.LaneInfo{laneInfo("a", "first", "app", 5, time.Hour)}
	m := newTestModel(t, forestOf(lanes, nil))
	if strings.Contains(plain(m.render()), "· live") {
		t.Error("a map without a watcher must not claim to be live")
	}
	if m.Init() == nil {
		t.Error("Init should still ask for the background colour")
	}
}

func launcherModel(t *testing.T, spawn func(string) error) Model {
	t.Helper()
	lanes := []index.LaneInfo{laneInfo("abc123def456", "queue stall", "app", 5, time.Hour)}
	m := NewModel(forestOf(lanes, nil), Options{
		ASCII: true,
		ResumeCommand: func(id string) (string, error) {
			return `claude --resume ` + id + ` --name "queue stall"`, nil
		},
		Spawn:     spawn,
		Terminal:  "tmux",
		LoadSpine: func(string) ([]graph.Segment, error) { return nil, nil },
	})
	m.now = func() time.Time { return now }
	m.width, m.height = 90, 20
	return m
}

func TestCopyResumePutsTheCommandOnTheClipboard(t *testing.T) {
	m := launcherModel(t, nil)
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	m = updated.(Model)

	if cmd == nil {
		t.Fatal("y should issue a clipboard command")
	}
	// The message type is internal to bubbletea; asserting it is not nil and
	// is not a plain nil-returning command is as far as a test can honestly go.
	if cmd() == nil {
		t.Error("the clipboard command produced no message")
	}
	if out := plain(m.render()); !strings.Contains(out, "copied: claude --resume abc123def456") {
		t.Errorf("expected the command in the status line:\n%s", out)
	}
}

func TestOpenTerminalWithoutAConfigCopiesInstead(t *testing.T) {
	m := launcherModel(t, nil)
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	m = updated.(Model)

	if cmd == nil {
		t.Error("with no launcher configured, o should still copy the command")
	}
	out := plain(m.render())
	if !strings.Contains(out, "BRAIDS_SPAWN") {
		t.Errorf("expected an explanation of how to configure one:\n%s", out)
	}
}

func TestOpenTerminalUsesTheConfiguredLauncher(t *testing.T) {
	var launched string
	m := launcherModel(t, func(id string) error {
		launched = id
		return nil
	})
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	m = updated.(Model)

	if launched != "abc123def456" {
		t.Errorf("launcher called with %q", launched)
	}
	if !strings.Contains(plain(m.render()), "opened abc123de in tmux") {
		t.Errorf("confirmation should name the terminal:\n%s", plain(m.render()))
	}
}

func TestLauncherFailureIsReported(t *testing.T) {
	m := launcherModel(t, func(string) error { return errors.New("tmux is not running") })
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	out := plain(updated.(Model).render())
	if !strings.Contains(out, "could not open a terminal") || !strings.Contains(out, "tmux is not running") {
		t.Errorf("a failed launch must be reported plainly:\n%s", out)
	}
}

func TestCopyResumeWorksFromTheSpineToo(t *testing.T) {
	m := launcherModel(t, nil)
	m = m.openSpine()
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd == nil {
		t.Fatal("y in the spine should also copy")
	}
	if !strings.Contains(plain(updated.(Model).renderSpine()), "copied:") {
		t.Error("the spine should confirm the copy")
	}
}

func TestSlashSearchesAndFFilters(t *testing.T) {
	lanes := []index.LaneInfo{
		laneInfo("a", "gcsfuse density", "app", 5, time.Hour),
		laneInfo("b", "schema refactor", "app", 5, time.Hour),
	}
	m := NewModel(forestOf(lanes, nil), Options{
		ASCII:  true,
		Search: func(string, string) ([]index.Hit, error) { return nil, nil },
	})
	m.now = func() time.Time { return now }
	m.width, m.height = 90, 20

	// / is the front door: full text across every conversation.
	slash, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	if slash.(Model).mode != searchMode {
		t.Fatal("/ should open search")
	}

	// f narrows the list in front of you, which is a different job.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = updated.(Model)
	if !m.filter.active {
		t.Fatal("f should open the lane filter")
	}
	for _, k := range []string{"s", "c", "h"} {
		updated, _ := m.Update(tea.KeyPressMsg{Code: rune(k[0]), Text: k})
		m = updated.(Model)
	}
	if len(m.visible) != 1 || m.visible[0].node.Lane.ID != "b" {
		t.Errorf("filter left %d lanes, want the schema one", len(m.visible))
	}
}

func TestPasteGoesToWhicheverFieldIsOpen(t *testing.T) {
	lanes := []index.LaneInfo{
		laneInfo("a", "gcsfuse density", "app", 5, time.Hour),
		laneInfo("b", "schema refactor", "app", 5, time.Hour),
	}
	newModel := func() Model {
		m := NewModel(forestOf(lanes, nil), Options{
			ASCII: true,
			LoadSpine: func(string) ([]graph.Segment, error) {
				return []graph.Segment{{Kind: graph.SegTurn, Seq: 1, Preview: "schema work"}}, nil
			},
			Branch: func(string, int, string, bool) (string, error) { return "x", nil },
		})
		m.now = func() time.Time { return now }
		m.width, m.height = 90, 20
		return m
	}

	t.Run("lane filter", func(t *testing.T) {
		m := newModel()
		updated, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
		m = updated.(Model)
		updated, _ = m.Update(tea.PasteMsg{Content: "schema"})
		m = updated.(Model)
		if len(m.visible) != 1 || m.visible[0].node.Lane.ID != "b" {
			t.Errorf("paste did not narrow the list: %d lanes", len(m.visible))
		}
	})

	t.Run("branch name", func(t *testing.T) {
		m := newModel().openSpine()
		m, _ = m.spineKey("b")
		m.spine.naming.text = ""
		updated, _ := m.Update(tea.PasteMsg{Content: "pasted-name"})
		m = updated.(Model)
		if got := m.spine.naming.text; got != "pasted-name" {
			t.Errorf("branch name = %q", got)
		}
	})

	t.Run("nothing open", func(t *testing.T) {
		m := newModel()
		updated, _ := m.Update(tea.PasteMsg{Content: "stray"})
		if len(updated.(Model).visible) != 2 {
			t.Error("a paste with no field open should change nothing")
		}
	})
}

func TestClearingTheLaneFilterKeepsYourPlace(t *testing.T) {
	lanes := []index.LaneInfo{
		laneInfo("a", "gcsfuse density", "app", 5, time.Hour),
		laneInfo("b", "schema refactor", "app", 5, time.Hour),
		laneInfo("c", "halt behaviour", "app", 5, time.Hour),
	}
	m := newTestModel(t, forestOf(lanes, nil))

	press := func(m Model, keys ...string) Model {
		for _, k := range keys {
			updated, _ := m.Update(tea.KeyPressMsg{Code: rune(k[0]), Text: k})
			m = updated.(Model)
		}
		return m
	}
	m = press(m, "f", "h", "a", "l", "t")
	if len(m.visible) != 1 {
		t.Fatalf("filter left %d lanes, want 1", len(m.visible))
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)
	if len(m.visible) != 3 {
		t.Fatalf("clearing should restore all lanes, got %d", len(m.visible))
	}
	if got := m.visible[m.cursor].node.Lane.ID; got != "c" {
		t.Errorf("cursor on %q after clearing, want the lane that was found", got)
	}
}

func TestNextWaitingSkipsWhatIsNotOwed(t *testing.T) {
	quiet := laneInfo("a", "finished long ago", "app", 5, 30*24*time.Hour)
	quiet.Activity = model.Activity{LastRole: model.RoleAssistant}
	owed := laneInfo("b", "answered just now", "app", 5, time.Minute)
	owed.Activity = model.Activity{LastRole: model.RoleAssistant}
	busy := laneInfo("c", "mid tool call", "app", 5, time.Second)
	busy.Activity = model.Activity{LastRole: model.RoleAssistant, LastWasToolCall: true}

	m := newTestModel(t, forestOf([]index.LaneInfo{quiet, owed, busy}, nil))
	if got := m.waitingCount(); got != 1 {
		t.Errorf("waitingCount = %d, want 1", got)
	}
	if got := m.nextWaiting(0, 1); got != 1 {
		t.Errorf("next waiting = %d, want the answered lane at 1", got)
	}
	// Wraps rather than stopping at the end.
	if got := m.nextWaiting(1, 1); got != 1 {
		t.Errorf("with one waiting lane the cursor should stay, got %d", got)
	}
	if !strings.Contains(plain(m.render()), "your turn") {
		t.Error("the status column should name the state")
	}
}

func TestSingleStepMovesWrapAround(t *testing.T) {
	lanes := []index.LaneInfo{
		laneInfo("a", "first", "app", 5, time.Hour),
		laneInfo("b", "second", "app", 5, time.Hour),
		laneInfo("c", "third", "app", 5, time.Hour),
	}
	m := newTestModel(t, forestOf(lanes, nil))
	press := func(m Model, key string) Model {
		updated, _ := m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
		return updated.(Model)
	}

	// Up from the top lands at the bottom.
	m = press(m, "k")
	if m.cursor != 2 {
		t.Errorf("k from the top = %d, want the last row", m.cursor)
	}
	// Down from the bottom lands at the top.
	m = press(m, "j")
	if m.cursor != 0 {
		t.Errorf("j from the bottom = %d, want the first row", m.cursor)
	}
}

func TestWrap(t *testing.T) {
	tests := []struct {
		cursor, delta, n, want int
	}{
		{0, -1, 3, 2},
		{2, 1, 3, 0},
		{1, 1, 3, 2},
		{0, 1, 1, 0},
		{0, -1, 0, 0}, // an empty list must not divide by zero
	}
	for _, tt := range tests {
		if got := wrap(tt.cursor, tt.delta, tt.n); got != tt.want {
			t.Errorf("wrap(%d,%d,%d) = %d, want %d", tt.cursor, tt.delta, tt.n, got, tt.want)
		}
	}
}

func TestGlyphKeyAppearsOnlyWhenThereIsRoom(t *testing.T) {
	lanes := []index.LaneInfo{laneInfo("a", "first", "app", 5, time.Hour)}
	m := newTestModel(t, forestOf(lanes, nil))

	m.width = 132
	wide := plain(m.render())
	for _, want := range []string{"stopped, needs you", "working", "branched from above"} {
		if !strings.Contains(wide, want) {
			t.Errorf("wide layout missing the glyph key %q:\n%s", want, wide)
		}
	}

	m.width = 90
	narrow := plain(m.render())
	if strings.Contains(narrow, "stopped, needs you") {
		t.Error("the glyph key should be dropped rather than squeezed")
	}
	if !strings.Contains(narrow, "search") {
		t.Error("the keys should survive after the glyph key is dropped")
	}
	// Whichever layout is chosen, nothing may overflow.
	for _, w := range []int{132, 110, 90, 70, 50, 30} {
		m.width = w
		for _, line := range strings.Split(plain(m.render()), "\n") {
			if got := len([]rune(line)); got > w {
				t.Errorf("at width %d a line ran to %d: %q", w, got, line)
			}
		}
	}
}

func TestKeyHintsNameBothDirections(t *testing.T) {
	lanes := []index.LaneInfo{laneInfo("a", "first", "app", 5, time.Hour)}
	m := newTestModel(t, forestOf(lanes, nil))
	m.width = 132
	out := plain(m.render())
	// "n/N next waiting" left it unsaid which of the two goes backwards.
	if !strings.Contains(out, "next / prev waiting") {
		t.Errorf("a paired key should say what each half does:\n%s", out)
	}
}

func TestGlyphKeyUsesTheStylesItExplains(t *testing.T) {
	lanes := []index.LaneInfo{laneInfo("a", "first", "app", 5, time.Hour)}
	m := newTestModel(t, forestOf(lanes, nil))
	m.width = 132

	// A legend drawn in one flat colour teaches nothing, since colour here is
	// meaning: each mark must carry the style it is actually drawn in.
	styles := map[string]string{}
	for _, g := range m.mapGlyphs() {
		styles[g.meaning] = g.style.Render("x")
	}
	for _, pair := range [][2]string{
		{"working", "idle"}, {"an open loop", "idle"}, {"stopped, needs you", "an open loop"},
	} {
		if styles[pair[0]] == styles[pair[1]] {
			t.Errorf("%q and %q must not share a style in the key", pair[0], pair[1])
		}
	}

	// And those styles must be the ones the rows use.
	live := laneInfo("b", "busy", "app", 5, time.Second)
	live.Activity = model.Activity{LastRole: model.RoleAssistant, LastWasToolCall: true}
	if got := m.styleFor(live, stateOf(live, nil, now)).Render("x"); got != styles["working"] {
		t.Error("the key's working style is not the one a working lane is drawn with")
	}
}

func TestEveryBindingIsListedBeforeTheGlyphKey(t *testing.T) {
	lanes := []index.LaneInfo{laneInfo("a", "first", "app", 5, time.Hour)}
	m := newTestModel(t, forestOf(lanes, nil))
	count := func(out, sub string) int { return strings.Count(out, sub) }

	// Wide: every key and the glyph key.
	m.width = 132
	wide := plain(m.render())
	if got := count(wide, "<"); got != len(hints()) {
		t.Errorf("wide layout listed %d of %d keys", got, len(hints()))
	}
	if !strings.Contains(wide, "archived") {
		t.Error("wide layout should carry the glyph key too")
	}

	// Middling: the glyph key gives way so that every key still fits.
	m.width = 100
	middling := plain(m.render())
	if got := count(middling, "<"); got != len(hints()) {
		t.Errorf("a key was dropped before the glyph key: %d of %d listed", got, len(hints()))
	}
	if strings.Contains(middling, "live conversation") {
		t.Error("the glyph key should give way to the keys, not the other way round")
	}

	// Narrow: whatever survives must include moving, opening and quitting.
	m.width = 80
	narrow := plain(m.render())
	for _, want := range []string{"down / up", "open spine", "quit"} {
		if !strings.Contains(narrow, want) {
			t.Errorf("a one-column legend must keep %q:\n%s", want, narrow)
		}
	}
}

// Hooks are optional, so whether they are on has to be visible: without them
// the state column is inferred from the files and cannot tell a tool that is
// working from one that is waiting on a person.
func TestHooksFactSaysWhichModeItIsIn(t *testing.T) {
	lanes := []index.LaneInfo{laneInfo("root", "main work", "app", 100, time.Hour)}
	f := forestOf(lanes, nil)

	for _, tc := range []struct {
		reporting bool
		want      string
	}{
		{true, "reporting"},
		{false, "off · see braids hooks"},
	} {
		m := NewModel(f, Options{ASCII: true, Source: "claudecode", Reporting: tc.reporting})
		m.now = func() time.Time { return now }
		m.width, m.height = 90, 24
		line := rowFor(t, plain(m.render()), "Hooks")
		if !strings.Contains(line, tc.want) {
			t.Errorf("reporting=%v shows %q, want it to mention %q", tc.reporting, line, tc.want)
		}
	}
}

// The panel is a box, so its three horizontal edges have to be the same width.
// A top border one column short reads as a broken corner.
func TestPanelBordersAreFlush(t *testing.T) {
	lanes := []index.LaneInfo{laneInfo("root", "main work", "app", 100, time.Hour)}
	f := forestOf(lanes, nil)
	for _, width := range []int{60, 84, 92, 120, 200} {
		m := NewModel(f, Options{ASCII: true, Source: "claudecode"})
		m.now = func() time.Time { return now }
		m.width, m.height = width, 24
		top := lipgloss.Width(plain(m.panelTop()))
		bottom := lipgloss.Width(plain(m.panelBottom()))
		row := lipgloss.Width(plain(m.framed(strings.Repeat(" ", width-2))))
		if top != width || bottom != width || row != width {
			t.Errorf("width %d: top=%d bottom=%d row=%d, want all %d", width, top, bottom, row, width)
		}
	}
}

// The header must never be wider than the terminal: it is drawn before the
// table, so an overrun pushes every row out of true.
func TestHeaderNeverExceedsTheTerminal(t *testing.T) {
	lanes := []index.LaneInfo{laneInfo("root", "main work", "app", 100, time.Hour)}
	f := forestOf(lanes, nil)
	for width := 50; width <= 240; width += 7 {
		m := NewModel(f, Options{ASCII: true, Source: "claudecode", IndexPath: "~/.braids/index.db"})
		m.now = func() time.Time { return now }
		m.width, m.height = width, 30
		for i, line := range strings.Split(plain(m.info()), "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width %d: header line %d is %d columns:\n%q", width, i, got, line)
			}
		}
	}
}

// The mark takes room before the legend spreads out, but never before every
// binding has a place. A key that works and is not listed may as well not exist.
func TestMarkNeverCostsABinding(t *testing.T) {
	lanes := []index.LaneInfo{laneInfo("root", "main work", "app", 100, time.Hour)}
	f := forestOf(lanes, nil)
	for width := 50; width <= 240; width += 3 {
		m := NewModel(f, Options{ASCII: true, Source: "claudecode"})
		m.now = func() time.Time { return now }
		m.width, m.height = width, 30
		p := m.headerPlan()
		if p.logo == nil {
			continue
		}
		if p.columns*p.rows < len(hints()) {
			t.Errorf("width %d: mark shown but only %d of %d bindings fit",
				width, p.columns*p.rows, len(hints()))
		}
	}
}

// Wider terminals get the larger mark; narrow ones get none rather than a
// cropped one.
func TestMarkSizeFollowsWidth(t *testing.T) {
	lanes := []index.LaneInfo{laneInfo("root", "main work", "app", 100, time.Hour)}
	f := forestOf(lanes, nil)
	seen := map[int]bool{}
	for width := 60; width <= 260; width += 2 {
		m := NewModel(f, Options{ASCII: true, Source: "claudecode"})
		m.now = func() time.Time { return now }
		m.width, m.height = width, 30
		seen[brand.Width(m.headerPlan().logo)] = true
	}
	for _, want := range []int{0, brand.Width(brand.Small()), brand.Width(brand.Full())} {
		if !seen[want] {
			t.Errorf("no width produced a mark of %d columns; saw %v", want, seen)
		}
	}
}

// The mark is decoration and priced below both legends: it may never cost the
// glyph key, which names marks that are on the screen right now.
func TestMarkNeverCostsTheGlyphKey(t *testing.T) {
	lanes := []index.LaneInfo{laneInfo("root", "main work", "app", 100, time.Hour)}
	f := forestOf(lanes, nil)
	shown := 0
	for width := 60; width <= 260; width++ {
		m := NewModel(f, Options{ASCII: true, Source: "claudecode"})
		m.now = func() time.Time { return now }
		m.width, m.height = width, 30
		p := m.headerPlan()
		if p.logo == nil {
			continue
		}
		shown++
		if len(m.mapGlyphs()) > 0 && !p.showGlyphs {
			t.Errorf("width %d: mark shown but the glyph key was dropped for it", width)
		}
	}
	if shown == 0 {
		t.Fatal("the mark never appeared at any width")
	}
}

// A terminal can report a size the layout is not defined for — a one-column
// pane, or a resize caught mid-flight. The width arithmetic subtracts borders
// and margins, so an unclamped small frame took the program down. No size may
// panic, in any view.
func TestNoFrameSizePanics(t *testing.T) {
	lanes := []index.LaneInfo{
		laneInfo("root", "main work", "app", 100, time.Hour),
		laneInfo("kid", "a branch", "app", 10, time.Minute),
	}
	f := forestOf(lanes, map[string]string{"kid": "root"})
	for width := 1; width <= 120; width++ {
		for _, height := range []int{1, 2, 5, 12, 40} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("panic at %dx%d: %v", width, height, r)
					}
				}()
				_ = Render(f, Options{ASCII: true, Source: "claudecode"}, "", "", width, height)
				_ = Render(f, Options{ASCII: true, Source: "claudecode"}, "root", "", width, height)
				_ = Render(f, Options{ASCII: true, Source: "claudecode"}, "", "work", width, height)
			}()
		}
	}
}

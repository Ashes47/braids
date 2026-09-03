package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Ashes47/braids/internal/core/graph"
	"github.com/Ashes47/braids/internal/core/index"
	"github.com/Ashes47/braids/internal/core/model"
)

func demoSegments() []graph.Segment {
	return []graph.Segment{
		{Kind: graph.SegTurn, Seq: 1, Role: model.RoleUser, Preview: "why is the queue stalling"},
		{Kind: graph.SegRun, Seq: 2, Count: 12, Tally: []graph.ToolCount{{Tool: "Bash", Count: 9}, {Tool: "Read", Count: 2}}},
		{Kind: graph.SegTurn, Seq: 14, Role: model.RoleUser, Preview: "try option c", Alternates: []int{6, 2}},
		{Kind: graph.SegTurn, Seq: 15, Role: model.RoleAssistant, Preview: "here is what I found"},
	}
}

func spineModel(t *testing.T, segs []graph.Segment, loadErr error) Model {
	t.Helper()
	lanes := []index.LaneInfo{laneInfo("lane1234abcd", "queue stall", "app", 40, time.Hour)}
	m := NewModel(forestOf(lanes, nil), Options{
		ASCII:     true,
		Source:    "claudecode",
		IndexPath: "/tmp/index.db",
		LoadSpine: func(string) ([]graph.Segment, error) { return segs, loadErr },
	})
	m.now = func() time.Time { return now }
	m.width, m.height = 90, 20
	return m
}

func TestEnterOpensTheSpineAndEscapeReturns(t *testing.T) {
	m := spineModel(t, demoSegments(), nil)

	opened, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = opened.(Model)
	if m.mode != spineMode || m.spine == nil {
		t.Fatal("enter should open the spine")
	}
	out := plain(m.View().Content)
	for _, want := range []string{
		"Lane:", "lane1234", "Junctions:", "Spine(queue stall)[4]",
		"why is the queue stalling", "12 turns · 9 Bash · 2 Read", "try option c",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("spine missing %q:\n%s", want, out)
		}
	}

	back, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = back.(Model)
	if m.mode != mapMode || m.spine != nil {
		t.Error("escape should return to the map")
	}
	if !strings.Contains(plain(m.View().Content), "Conversations(all)") {
		t.Error("map should be showing again")
	}
}

func TestSpineRowsFitTheWidth(t *testing.T) {
	m := spineModel(t, demoSegments(), nil)
	m = m.openSpine()
	for _, line := range strings.Split(plain(m.renderSpine()), "\n") {
		if w := len([]rune(line)); w > m.width {
			t.Errorf("line exceeds width %d (%d): %q", m.width, w, line)
		}
	}
}

func TestSpineShowsBranchCount(t *testing.T) {
	m := spineModel(t, demoSegments(), nil)
	m = m.openSpine()
	// The junction turn reports its two departing branches.
	if !strings.Contains(plain(m.renderSpine()), "|- 2") {
		t.Errorf("expected a branch marker on the junction:\n%s", plain(m.renderSpine()))
	}
}

func TestSpineSurfacesLoadFailure(t *testing.T) {
	m := spineModel(t, nil, errors.New("index is unreadable"))
	m = m.openSpine()
	if !strings.Contains(plain(m.renderSpine()), "index is unreadable") {
		t.Error("a failed load should be shown, not swallowed")
	}
}

func TestSpineEmptyLane(t *testing.T) {
	m := spineModel(t, nil, nil)
	m = m.openSpine()
	if !strings.Contains(plain(m.renderSpine()), "no turns") {
		t.Error("an empty lane should say so")
	}
}

func TestNextJunctionWrapsBothWays(t *testing.T) {
	segs := []spineRow{
		{seg: graph.Segment{Kind: graph.SegTurn}},
		{seg: graph.Segment{Kind: graph.SegTurn, Alternates: []int{1}}},
		{seg: graph.Segment{Kind: graph.SegTurn}},
		{seg: graph.Segment{Kind: graph.SegTurn, Alternates: []int{2}}},
	}
	if got := nextJunction(segs, 0, 1); got != 1 {
		t.Errorf("forward from 0 = %d, want 1", got)
	}
	if got := nextJunction(segs, 1, 1); got != 3 {
		t.Errorf("forward from 1 = %d, want 3", got)
	}
	if got := nextJunction(segs, 3, 1); got != 1 {
		t.Errorf("forward from the last junction should wrap to 1, got %d", got)
	}
	if got := nextJunction(segs, 0, -1); got != 3 {
		t.Errorf("backward from 0 should wrap to 3, got %d", got)
	}
	if got := nextJunction([]spineRow{{seg: graph.Segment{Kind: graph.SegTurn}}}, 0, 1); got != 0 {
		t.Errorf("with no junctions the cursor must not move, got %d", got)
	}
}

func TestQuitWorksFromEitherScreen(t *testing.T) {
	m := spineModel(t, demoSegments(), nil)
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"}); cmd == nil {
		t.Error("q should quit from the map")
	}
	m = m.openSpine()
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"}); cmd == nil {
		t.Error("q should quit from the spine")
	}
}

func TestSpineFilterNarrowsAndTitles(t *testing.T) {
	m := spineModel(t, demoSegments(), nil)
	m = m.openSpine()

	m = m.spineKey("/")
	for _, k := range []string{"o", "p", "t", "i", "o", "n"} {
		m = m.spineKey(k)
	}
	if len(m.spine.visible) != 1 {
		t.Fatalf("filter left %d rows, want 1", len(m.spine.visible))
	}
	out := plain(m.renderSpine())
	if !strings.Contains(out, "Spine(queue stall)(/option)[1]") {
		t.Errorf("panel title should carry the filter:\n%s", out)
	}
	if strings.Contains(out, "why is the queue stalling") {
		t.Error("filtered-out segment still drawn")
	}

	// Runs are matched on their summary, so a tool name finds them.
	m = m.spineKey("esc")
	m = m.spineKey("/")
	for _, k := range []string{"b", "a", "s", "h"} {
		m = m.spineKey(k)
	}
	if len(m.spine.visible) != 1 || m.spine.visible[0].seg.Kind != graph.SegRun {
		t.Errorf("filtering by tool should find the run, got %+v", m.spine.visible)
	}
}

func TestSpineEscapePeelsFilterBeforeLeaving(t *testing.T) {
	m := spineModel(t, demoSegments(), nil)
	m = m.openSpine()
	m = m.spineKey("/")
	m = m.spineKey("o")

	m = m.spineKey("esc")
	if m.mode != spineMode {
		t.Fatal("the first esc should clear the filter, not leave the spine")
	}
	if m.spine.filter.on() {
		t.Error("filter should be cleared")
	}
	m = m.spineKey("esc")
	if m.mode != mapMode {
		t.Error("the second esc should return to the map")
	}
}

func TestTypingQInAFilterDoesNotQuit(t *testing.T) {
	m := spineModel(t, demoSegments(), nil)
	m = m.openSpine()
	m = m.spineKey("/")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd != nil {
		t.Fatal("q while filtering must type a letter, not quit")
	}
	if got := updated.(Model).spine.filter.text; got != "q" {
		t.Errorf("filter text = %q, want %q", got, "q")
	}
}

func TestSpineFilterWithNoMatchesExplainsItself(t *testing.T) {
	m := spineModel(t, demoSegments(), nil)
	m = m.openSpine()
	m.spine.filter.text = "zzzz"
	m.spine.apply()
	if !strings.Contains(plain(m.renderSpine()), `nothing matches "zzzz"`) {
		t.Error("expected an empty-state message in the spine")
	}
}

func TestBranchFromASpineTurn(t *testing.T) {
	var gotLane, gotName string
	var gotTurn int
	m := spineModel(t, demoSegments(), nil)
	m.branch = func(lane string, turn int, name string) (string, error) {
		gotLane, gotTurn, gotName = lane, turn, name
		return "newlane01234", nil
	}
	m = m.openSpine()
	m = m.spineKey("j") // move to the collapsed run

	m = m.spineKey("b")
	if !strings.Contains(plain(m.renderSpine()), "collapsed run") {
		t.Error("branching from a run should explain why it cannot")
	}

	m = m.spineKey("k") // back to the first turn
	m = m.spineKey("b")
	if !m.spine.naming.active {
		t.Fatal("b should open the name field")
	}
	if got := m.spine.naming.text; got != "queue-stalling" {
		t.Errorf("suggested name = %q", got)
	}
	// The suggestion is editable.
	m = m.spineKey("backspace")
	m = m.spineKey("!")
	m = m.spineKey("enter")

	if gotLane != "lane1234abcd" || gotTurn != 1 || gotName != "queue-stallin!" {
		t.Errorf("branch called with (%q, %d, %q)", gotLane, gotTurn, gotName)
	}
	out := plain(m.renderSpine())
	if !strings.Contains(out, "branched at t1") || !strings.Contains(out, "newlane") {
		t.Errorf("expected a confirmation:\n%s", out)
	}
}

func TestBranchFailureIsShown(t *testing.T) {
	m := spineModel(t, demoSegments(), nil)
	m.branch = func(string, int, string) (string, error) { return "", errors.New("disk is full") }
	m = m.openSpine()
	m = m.spineKey("b")
	m = m.spineKey("enter")
	if !strings.Contains(plain(m.renderSpine()), "disk is full") {
		t.Error("a failed branch must be reported")
	}
}

func TestBranchUnavailableWithoutASource(t *testing.T) {
	m := spineModel(t, demoSegments(), nil)
	m.branch = nil
	m = m.openSpine()
	m = m.spineKey("b")
	if m.spine.naming.active {
		t.Error("the name field should not open when branching is unavailable")
	}
	if !strings.Contains(plain(m.renderSpine()), "unavailable") {
		t.Error("expected an explanation")
	}
}

func TestSuggestName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"why is the queue stalling", "queue-stalling"},
		{"", "branch-t7"},
		{"the and for with this", "branch-t7"},
		{"NULLS LAST starved the unstamped backlog", "nulls-last-starved"},
	}
	for _, tt := range tests {
		got := suggestName(graph.Segment{Seq: 7, Preview: tt.in})
		if got != tt.want {
			t.Errorf("suggestName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// forkChild attaches a child lane to a node, as the forest would.
func forkChild(parent *graph.Node, id, title string, msgs, forkSeq int) *graph.Node {
	li := laneInfo(id, title, "app", msgs, time.Hour)
	child := &graph.Node{Lane: li, ParentID: parent.Lane.ID, ForkSeq: forkSeq, Depth: 1}
	parent.Children = append(parent.Children, child)
	return child
}

func TestSpineShowsForksInlineAtTheirTurn(t *testing.T) {
	m := spineModel(t, demoSegments(), nil)
	node := m.visible[0].node
	forkChild(node, "kid00000001", "try another way", 9, 1)

	m = m.openSpine()
	out := plain(m.renderSpine())
	if !strings.Contains(out, "try another way") {
		t.Fatalf("a branch that left this lane must appear in it:\n%s", out)
	}
	if !strings.Contains(out, "(9 turns)") || !strings.Contains(out, "< t1") {
		t.Errorf("fork row should carry its size and fork point:\n%s", out)
	}

	// It belongs directly after the turn it left from, not at the end.
	rows := m.spine.rows
	if rows[0].fork != nil || rows[1].fork == nil {
		t.Errorf("fork placed at row %d, want immediately after t1", indexOfFork(rows))
	}
	if !strings.Contains(out, "2 kept here · 1 forked out") {
		t.Errorf("facts should count lanes that forked out:\n%s", out)
	}
}

func indexOfFork(rows []spineRow) int {
	for i, r := range rows {
		if r.fork != nil {
			return i
		}
	}
	return -1
}

func TestEnterDescendsIntoABranchAndEscapeComesBack(t *testing.T) {
	m := spineModel(t, demoSegments(), nil)
	node := m.visible[0].node
	forkChild(node, "kid00000001", "the branch", 9, 1)

	m = m.openSpine()
	m = m.spineKey("j") // onto the fork row
	if m.spine.current().fork == nil {
		t.Fatal("expected the cursor on the fork row")
	}

	m = m.spineKey("enter")
	if m.spine.lane.ID != "kid00000001" {
		t.Fatalf("enter should open the branch, got lane %q", m.spine.lane.ID)
	}
	if len(m.stack) != 1 {
		t.Fatalf("descending should remember where we came from, stack = %d", len(m.stack))
	}

	m = m.spineKey("esc")
	if m.spine == nil || m.spine.lane.ID != "lane1234abcd" {
		t.Fatal("esc should return to the parent conversation, not the map")
	}
	m = m.spineKey("esc")
	if m.mode != mapMode {
		t.Error("a second esc should return to the map")
	}
}

func TestBranchingFromAForkRowExplainsItself(t *testing.T) {
	m := spineModel(t, demoSegments(), nil)
	forkChild(m.visible[0].node, "kid00000001", "the branch", 9, 1)
	m.branch = func(string, int, string) (string, error) { return "x", nil }

	m = m.openSpine()
	m = m.spineKey("j")
	m = m.spineKey("b")
	if m.spine.naming.active {
		t.Error("b on a fork row should not open the name field")
	}
	if !strings.Contains(plain(m.renderSpine()), "press enter to open that branch") {
		t.Error("expected guidance towards enter")
	}
}

func TestNextSplitFindsForksAsWellAsJunctions(t *testing.T) {
	rows := []spineRow{
		{seg: graph.Segment{Kind: graph.SegTurn}},
		{fork: &graph.Node{}},
		{seg: graph.Segment{Kind: graph.SegTurn}},
	}
	if got := nextJunction(rows, 0, 1); got != 1 {
		t.Errorf("next split = %d, want the fork row at 1", got)
	}
}

func TestDescribeBranches(t *testing.T) {
	tests := []struct {
		kept, forked int
		want         string
	}{
		{0, 0, "none"},
		{3, 0, "3 kept here"},
		{0, 2, "2 forked out"},
		{3, 2, "3 kept here · 2 forked out"},
	}
	for _, tt := range tests {
		if got := describeBranches(tt.kept, tt.forked); got != tt.want {
			t.Errorf("describeBranches(%d,%d) = %q, want %q", tt.kept, tt.forked, got, tt.want)
		}
	}
}

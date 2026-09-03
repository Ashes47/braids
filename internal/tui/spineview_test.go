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
	segs := []graph.Segment{
		{Kind: graph.SegTurn},
		{Kind: graph.SegTurn, Alternates: []int{1}},
		{Kind: graph.SegTurn},
		{Kind: graph.SegTurn, Alternates: []int{2}},
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
	if got := nextJunction([]graph.Segment{{Kind: graph.SegTurn}}, 0, 1); got != 0 {
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

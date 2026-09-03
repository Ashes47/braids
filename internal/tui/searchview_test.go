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

func searchModel(t *testing.T, hits []index.Hit, err error) (Model, *[]string) {
	t.Helper()
	lanes := []index.LaneInfo{
		laneInfo("main00000001", "nvidia delivery", "app", 400, time.Hour),
		laneInfo("side00000002", "schema refactor", "app", 20, time.Hour),
	}
	queries := &[]string{}
	m := NewModel(forestOf(lanes, nil), Options{
		ASCII: true,
		LoadSpine: func(string) ([]graph.Segment, error) {
			return []graph.Segment{
				{Kind: graph.SegTurn, Seq: 1, Role: model.RoleUser, Preview: "start"},
				{Kind: graph.SegRun, Seq: 2, Count: 10},
				{Kind: graph.SegTurn, Seq: 12, Role: model.RoleUser, Preview: "the matching turn"},
				{Kind: graph.SegTurn, Seq: 13, Role: model.RoleAssistant, Preview: "after"},
			}, nil
		},
		Search: func(q, scope string) ([]index.Hit, error) {
			*queries = append(*queries, q+"|"+scope)
			return hits, err
		},
	})
	m.now = func() time.Time { return now }
	m.width, m.height = 92, 20
	return m, queries
}

func demoHits() []index.Hit {
	return []index.Hit{
		{LaneID: "main00000001", LaneTitle: "nvidia delivery", Seq: 12, Kind: model.PartText,
			Snippet: "the [gcsfuse] mount is hard-coded"},
		{LaneID: "side00000002", LaneTitle: "schema refactor", Seq: 4, Kind: model.PartToolUse,
			Tool: "Bash", Snippet: "mount | grep [gcsfuse]"},
	}
}

func typeSearch(m Model, keys ...string) Model {
	for _, k := range keys {
		m = m.searchKey(k)
	}
	return m
}

func TestSlashOpensSearchAndTypingQueries(t *testing.T) {
	m, queries := searchModel(t, demoHits(), nil)
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(Model)
	if m.mode != searchMode {
		t.Fatal("/ should open search")
	}
	m = typeSearch(m, "g", "c", "s")

	if len(*queries) != 3 || (*queries)[2] != "gcs|" {
		t.Errorf("queries = %v, want one per keystroke scoped to everything", *queries)
	}
	out := plain(m.renderSearch())
	for _, want := range []string{
		"Query:", "gcs", "Scope:", "every conversation", "Hits:", "Search(everywhere)[2]",
		"nvidia delivery", "t12", "the [gcsfuse] mount", "Bash", "jump to turn",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("search screen missing %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if w := len([]rune(line)); w > m.width {
			t.Errorf("line exceeds width %d (%d): %q", m.width, w, line)
		}
	}
}

func TestSearchScopeTogglesToTheOpenConversation(t *testing.T) {
	m, queries := searchModel(t, demoHits(), nil)
	m = m.openSpine() // reading main00000001
	m = m.openSearch()
	if m.search.scope != "main00000001" {
		t.Fatalf("searching from a conversation should start scoped to it, got %q", m.search.scope)
	}
	m = typeSearch(m, "x", "tab")
	if m.search.scope != "" {
		t.Error("tab should widen the scope to everything")
	}
	m = typeSearch(m, "tab")
	if m.search.scope != "main00000001" {
		t.Error("tab should narrow back to the conversation")
	}
	if len(*queries) == 0 || !strings.HasSuffix((*queries)[0], "|main00000001") {
		t.Errorf("first query was not scoped: %v", *queries)
	}
}

func TestEnterJumpsToTheTurnAndShowsItsThread(t *testing.T) {
	m, _ := searchModel(t, demoHits(), nil)
	m = m.openSearch()
	m = typeSearch(m, "g")
	m = typeSearch(m, "enter")

	if m.mode != spineMode || m.spine == nil {
		t.Fatal("enter should open the conversation the result is in")
	}
	if m.spine.lane.ID != "main00000001" {
		t.Fatalf("landed in %q", m.spine.lane.ID)
	}
	// t12 is a turn of its own; the cursor must be on it, not at the top.
	if got := m.spine.visible[m.spine.cursor].seg.Seq; got != 12 {
		t.Errorf("cursor on t%d, want t12", got)
	}
	if !strings.Contains(plain(m.renderSpine()), "jumped to t12") {
		t.Error("expected confirmation of where we landed")
	}
}

func TestJumpLandsOnTheRunThatSwallowedTheTurn(t *testing.T) {
	hits := []index.Hit{{LaneID: "main00000001", LaneTitle: "nvidia delivery", Seq: 7, Kind: model.PartText}}
	m, _ := searchModel(t, hits, nil)
	m = m.openSearch()
	m = typeSearch(m, "g", "enter")

	// Turn 7 is inside the run that starts at t2, so that is where to land.
	if got := m.spine.visible[m.spine.cursor].seg.Seq; got != 2 {
		t.Errorf("cursor on t%d, want the run starting at t2", got)
	}
}

func TestSearchReportsFailureAndEmptiness(t *testing.T) {
	t.Run("no query yet", func(t *testing.T) {
		m, _ := searchModel(t, nil, nil)
		m = m.openSearch()
		if !strings.Contains(plain(m.renderSearch()), "type to search") {
			t.Error("an empty search should invite one")
		}
	})
	t.Run("no matches", func(t *testing.T) {
		m, _ := searchModel(t, nil, nil)
		m = m.openSearch()
		m = typeSearch(m, "z")
		if !strings.Contains(plain(m.renderSearch()), `nothing matches "z"`) {
			t.Error("expected an empty-state message")
		}
	})
	t.Run("query is malformed", func(t *testing.T) {
		m, _ := searchModel(t, nil, errors.New("fts5: syntax error"))
		m = m.openSearch()
		m = typeSearch(m, "\"")
		if !strings.Contains(plain(m.renderSearch()), "syntax error") {
			t.Error("a bad query should say so rather than look empty")
		}
	})
}

func TestEscapeReturnsWhereSearchWasOpenedFrom(t *testing.T) {
	m, _ := searchModel(t, demoHits(), nil)
	m = m.openSpine()
	m = m.openSearch()
	m = typeSearch(m, "esc")
	if m.mode != spineMode {
		t.Error("escaping a search opened from a conversation should return to it")
	}

	m = m.closeSpine()
	m = m.openSearch()
	m = typeSearch(m, "esc")
	if m.mode != mapMode {
		t.Error("escaping a search opened from the map should return to the map")
	}
}

func TestTypingQIntoSearchDoesNotQuit(t *testing.T) {
	m, _ := searchModel(t, demoHits(), nil)
	m = m.openSearch()
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd != nil {
		t.Fatal("q while searching must type a letter")
	}
	if got := updated.(Model).search.input.text; got != "q" {
		t.Errorf("query = %q", got)
	}
}

func TestPasteIntoSearch(t *testing.T) {
	m, queries := searchModel(t, demoHits(), nil)
	m = m.openSearch()
	m = typeSearch(m, "g")

	updated, _ := m.Update(tea.PasteMsg{Content: "csfuse\ndensity  "})
	m = updated.(Model)

	// A pasted newline must not become part of the query.
	if got := m.search.input.text; got != "gcsfuse density" {
		t.Errorf("query = %q, want %q", got, "gcsfuse density")
	}
	if len(*queries) == 0 || !strings.HasPrefix((*queries)[len(*queries)-1], "gcsfuse density|") {
		t.Errorf("paste did not re-run the search: %v", *queries)
	}
}

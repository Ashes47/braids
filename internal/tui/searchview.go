package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/Ashes47/braids/internal/core/index"
)

// Search is the front door. With thousands of turns across dozens of
// conversations, nobody finds the point they want by scrolling to it — they
// type two words they remember. The graph is what makes the answer meaningful
// afterwards: it says where you landed and what else grew from there.
type searchState struct {
	input   filterInput
	scope   string // empty searches everything; a lane ID narrows to one
	hits    []index.Hit
	elapsed time.Duration
	err     error
	cursor  int
	offset  int
}

const (
	hitLaneWidth = 22
	hitTurnWidth = 7
	hitKindWidth = 11
)

// openSearch starts a search, scoped to the conversation being read if any.
func (m Model) openSearch() Model {
	scope := ""
	if m.mode == spineMode && m.spine != nil {
		scope = m.spine.lane.ID
	}
	m.search = &searchState{input: filterInput{active: true}, scope: scope}
	m.returnTo = m.mode
	m.mode = searchMode
	return m
}

func (m Model) searchKey(key string) Model {
	s := m.search
	switch key {
	case "esc":
		m.mode = m.returnTo
		m.search = nil
		return m
	case "tab":
		s.scope = m.toggleScope(s.scope)
		m.runSearch()
		return m
	case "enter":
		return m.jumpToHit()
	case "down", "ctrl+n":
		s.cursor = wrap(s.cursor, 1, len(s.hits))
	case "up", "ctrl+p":
		s.cursor = wrap(s.cursor, -1, len(s.hits))
	case "backspace":
		s.input.edit(key)
		m.runSearch()
	default:
		if !s.input.edit(key) {
			return m
		}
		m.runSearch()
	}
	m.clampSearch()
	return m
}

// toggleScope flips between everything and the conversation we came from.
func (m Model) toggleScope(current string) string {
	if current != "" {
		return ""
	}
	if m.returnTo == spineMode && m.spine != nil {
		return m.spine.lane.ID
	}
	if lane, ok := m.selectedLane(); ok {
		return lane
	}
	return ""
}

func (m *Model) runSearch() {
	s := m.search
	s.cursor, s.offset = 0, 0
	if m.searchFn == nil || strings.TrimSpace(s.input.text) == "" {
		s.hits, s.err, s.elapsed = nil, nil, 0
		return
	}
	started := time.Now()
	s.hits, s.err = m.searchFn(s.input.text, s.scope)
	s.elapsed = time.Since(started)
}

func (m *Model) clampSearch() {
	s := m.search
	if s.cursor >= len(s.hits) {
		s.cursor = len(s.hits) - 1
	}
	if s.cursor < 0 {
		s.cursor = 0
	}
	h := m.bodyHeight()
	if s.cursor < s.offset {
		s.offset = s.cursor
	}
	if s.cursor >= s.offset+h {
		s.offset = s.cursor - h + 1
	}
	if s.offset < 0 {
		s.offset = 0
	}
}

// jumpToHit opens the conversation a result belongs to, positioned at the turn
// that matched. Landing somewhere and seeing the surrounding thread is the
// whole point: a result on its own says nothing about what came before it.
func (m Model) jumpToHit() Model {
	s := m.search
	if s.cursor >= len(s.hits) {
		return m
	}
	hit := s.hits[s.cursor]
	for i, r := range m.all {
		if r.node.Lane.ID != hit.LaneID {
			continue
		}
		m.search = nil
		m.stack = nil
		m.cursor = i
		m.clamp()
		m = m.openNode(r.node, false)
		m.spine.cursor = rowAtOrBefore(m.spine.visible, hit.Seq)
		m.clampSpine()
		m.spine.notice = fmt.Sprintf("jumped to t%d", hit.Seq)
		return m
	}
	s.err = fmt.Errorf("conversation %s is no longer indexed", shortID(hit.LaneID))
	return m
}

// rowAtOrBefore finds the row holding a turn, which may be a run that swallowed
// it rather than a line of its own.
func rowAtOrBefore(rows []spineRow, seq int) int {
	best := 0
	for i, r := range rows {
		if r.fork != nil {
			continue
		}
		if r.seg.Seq <= seq {
			best = i
			continue
		}
		break
	}
	return best
}

func (m Model) renderSearch() string {
	s := m.search
	var b strings.Builder
	b.WriteString(m.searchInfo())
	b.WriteString("\n\n")
	b.WriteString(m.panelTopTitled(m.searchTitle()))
	b.WriteString("\n")
	b.WriteString(m.framed(m.searchColumns()))
	b.WriteString("\n")

	blank := strings.Repeat(" ", m.contentWidth())
	switch {
	case s.err != nil:
		b.WriteString(m.framed(padRight(" "+m.theme.Empty.Render(s.err.Error()), m.contentWidth())))
		b.WriteString("\n")
		for range m.bodyHeight() - 1 {
			b.WriteString(m.framed(blank) + "\n")
		}
	case len(s.hits) == 0:
		b.WriteString(m.framed(padRight(" "+m.theme.Empty.Render(m.searchEmpty()), m.contentWidth())))
		b.WriteString("\n")
		for range m.bodyHeight() - 1 {
			b.WriteString(m.framed(blank) + "\n")
		}
	default:
		end := min(s.offset+m.bodyHeight(), len(s.hits))
		for i := s.offset; i < end; i++ {
			b.WriteString(m.framed(m.renderHit(s.hits[i], i == s.cursor)))
			b.WriteString("\n")
		}
		for range m.bodyHeight() - (end - s.offset) {
			b.WriteString(m.framed(blank) + "\n")
		}
	}
	b.WriteString(m.panelBottom())
	b.WriteString("\n")
	b.WriteString(" " + m.theme.Column.Render("/") + m.theme.Value.Render(s.input.text) +
		m.theme.Column.Render("▏") + "  " +
		m.theme.Label.Render("↵ jump · tab scope · esc back"))
	return b.String()
}

func (m Model) searchEmpty() string {
	if strings.TrimSpace(m.search.input.text) == "" {
		return "type to search every message and tool call"
	}
	return fmt.Sprintf("nothing matches %q", m.search.input.text)
}

func (m Model) searchTitle() string {
	scope := "everywhere"
	if m.search.scope != "" {
		scope = shortID(m.search.scope)
	}
	return fmt.Sprintf("Search(%s)[%d]", scope, len(m.search.hits))
}

func (m Model) searchInfo() string {
	s := m.search
	timing := "—"
	if s.elapsed > 0 {
		timing = s.elapsed.Round(time.Microsecond).String()
	}
	scope := "every conversation"
	if s.scope != "" {
		scope = "this conversation"
	}
	facts := []fact{
		{"Query", orDash(s.input.text)},
		{"Scope", scope},
		{"Hits", fmt.Sprintf("%d", len(s.hits))},
		{"Took", timing},
	}
	keys := []hint{
		{"↵", "jump to turn"}, {"tab", "change scope"},
		{"↑/↓", "move"}, {"esc", "back"},
	}
	return m.factsBlock(facts, keys)
}

// orDash keeps an empty fact readable rather than blank.
func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func (m Model) searchColumns() string {
	// Mirrors renderHit exactly: " " + lane + " " + turn + " " + kind + " " + match.
	nameWidth := m.contentWidth() - 4 - hitLaneWidth - hitTurnWidth - hitKindWidth
	if nameWidth < 8 {
		nameWidth = 8
	}
	return m.theme.Column.Render(" " + padRight("CONVERSATION", hitLaneWidth) + " " +
		padLeft("TURN", hitTurnWidth) + " " + padRight("KIND", hitKindWidth) + " " +
		padRight("MATCH", nameWidth))
}

func (m Model) renderHit(hit index.Hit, selected bool) string {
	lane := hit.LaneTitle
	if lane == "" {
		lane = shortID(hit.LaneID)
	}
	kind := string(hit.Kind)
	if hit.Tool != "" {
		kind = hit.Tool
	}
	snippetWidth := m.contentWidth() - 4 - hitLaneWidth - hitTurnWidth - hitKindWidth
	if snippetWidth < 8 {
		snippetWidth = 8
	}

	laneCell := padRight(truncate(lane, hitLaneWidth), hitLaneWidth)
	turnCell := padLeft(fmt.Sprintf("t%d", hit.Seq), hitTurnWidth)
	kindCell := padRight(truncate(kind, hitKindWidth), hitKindWidth)
	textCell := padRight(truncate(oneLine(hit.Snippet), snippetWidth), snippetWidth)

	plain := " " + laneCell + " " + turnCell + " " + kindCell + " " + textCell
	if selected {
		return m.theme.Selected.Width(m.contentWidth()).Render(plain)
	}
	return " " + m.theme.Value.Render(laneCell) + " " +
		m.theme.Faint.Render(turnCell) + " " +
		m.theme.Dim.Render(kindCell) + " " +
		m.theme.Title.Render(textCell)
}

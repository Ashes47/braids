package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

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
	// hitTypeWidth holds the longest label, "memory".
	hitTypeWidth = 6
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
	// Each kind of result opens on the screen that can act on it.
	switch hit.Of {
	case index.FoundMemory:
		return m.jumpToMemory(hit)
	case index.FoundArtifact:
		return m.jumpToArtifact(hit)
	}
	for i, r := range m.all {
		if r.node.Lane.ID != hit.LaneID {
			continue
		}
		m.search = nil
		m.stack = nil
		m.cursor = i
		m.clamp()
		m = m.openNode(r.node, false)
		if m.spine == nil {
			// The spine refused to open — no loader, or the transcript could
			// not be read. It has already said why; there is nothing to point
			// a cursor at.
			return m
		}
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

	blank := repeat(" ", m.contentWidth())
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
		m.theme.Label.Render("↵ open · tab scope · esc back"))
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
	return m.factsBlock(m.searchFacts(), searchHints(), nil)
}

// searchHints are every binding the search screen has.
func searchHints() []hint {
	return []hint{
		{"↵", "open the result"}, {"tab", "change scope"},
		{"↑ / ↓", "down / up"}, {"esc", "back"},
	}
}

func (m Model) searchFacts() []fact {
	s := m.search
	timing := "—"
	if s.elapsed > 0 {
		timing = s.elapsed.Round(time.Microsecond).String()
	}
	scope := "every conversation"
	if s.scope != "" {
		scope = "this conversation"
	}
	return []fact{
		{"Query", orDash(s.input.text)},
		{"Scope", scope},
		{"Hits", fmt.Sprintf("%d", len(s.hits))},
		{"Took", timing},
	}
}

// orDash keeps an empty fact readable rather than blank.
func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func (m Model) searchColumns() string {
	// Mirrors renderHit exactly: " " + type + " " + where + " " + turn + " "
	// + kind + " " + match.
	return m.theme.Column.Render(" " + padRight("TYPE", hitTypeWidth) + " " +
		padRight("WHERE", hitLaneWidth) + " " +
		padLeft("TURN", hitTurnWidth) + " " + padRight("KIND", hitKindWidth) + " " +
		padRight("MATCH", m.hitTextWidth()))
}

// hitTextWidth is what is left for the matched text.
func (m Model) hitTextWidth() int {
	return max(m.contentWidth()-5-hitTypeWidth-hitLaneWidth-hitTurnWidth-hitKindWidth, 8)
}

// hitWhere names the thing found: the conversation for a turn, the memory or
// the work product's path for the others.
func hitWhere(hit index.Hit) string {
	if !hit.IsTurn() && hit.Name != "" {
		return hit.Name
	}
	if hit.LaneTitle != "" {
		return hit.LaneTitle
	}
	return shortID(hit.LaneID)
}

// hitTypeLabel is the one-word column saying what a result is. Search covers
// conversations, memories and work products at once, and a result you cannot
// tell the kind of is one you have to open before you understand it.
func hitTypeLabel(hit index.Hit) string {
	switch hit.Of {
	case index.FoundMemory:
		return "memory"
	case index.FoundArtifact:
		return "work"
	default:
		return "convo"
	}
}

// hitTypeStyle colours the type, so the three kinds separate at a glance.
func (m Model) hitTypeStyle(hit index.Hit) lipgloss.Style {
	switch hit.Of {
	case index.FoundMemory:
		return m.theme.Accent
	case index.FoundArtifact:
		return m.theme.Label
	default:
		return m.theme.Dim
	}
}

func (m Model) renderHit(hit index.Hit, selected bool) string {
	kind := string(hit.Kind)
	if hit.Tool != "" {
		kind = hit.Tool
	}
	turn := ""
	if hit.IsTurn() {
		turn = fmt.Sprintf("t%d", hit.Seq)
	}

	typeCell := padRight(hitTypeLabel(hit), hitTypeWidth)
	laneCell := padRight(truncate(hitWhere(hit), hitLaneWidth), hitLaneWidth)
	turnCell := padLeft(turn, hitTurnWidth)
	kindCell := padRight(truncate(kind, hitKindWidth), hitKindWidth)
	textCell := padRight(truncate(oneLine(hit.Snippet), m.hitTextWidth()), m.hitTextWidth())

	plain := " " + typeCell + " " + laneCell + " " + turnCell + " " + kindCell + " " + textCell
	if selected {
		return m.theme.Selected.Width(m.contentWidth()).Render(plain)
	}
	return " " + m.hitTypeStyle(hit).Render(typeCell) + " " +
		m.theme.Value.Render(laneCell) + " " +
		m.theme.Faint.Render(turnCell) + " " +
		m.theme.Dim.Render(kindCell) + " " +
		m.theme.Title.Render(textCell)
}

// jumpToMemory opens the memory screen with the found memory under the cursor.
func (m Model) jumpToMemory(hit index.Hit) Model {
	if m.loadMemories == nil {
		m.search.err = errors.New("memories are unavailable")
		return m
	}
	m.search = nil
	m = m.openMemories()
	if m.memories == nil {
		return m
	}
	// Filtering to the name is what puts the cursor on it, and leaves the
	// filter visible so it is obvious why the list is short.
	m.memories.filter.text = hit.Name
	m.applyMemoryFilter()
	// Searching for a memory means wanting to read it, not to look at a row
	// describing it.
	if entry, ok := m.memoryCursor(); ok && entry.Name == hit.Name {
		return m.readMemory()
	}
	return m
}

// jumpToArtifact opens the work browser at the directory holding the file.
func (m Model) jumpToArtifact(hit index.Hit) Model {
	if m.loadWork == nil {
		m.search.err = errors.New("work products are unavailable")
		return m
	}
	for i, r := range m.all {
		if r.node.Lane.ID != hit.LaneID {
			continue
		}
		m.search = nil
		m.cursor = i
		m.clamp()
		m = m.openWork()
		if m.work == nil {
			return m
		}
		if dir := filepath.Dir(hit.Name); dir != "." {
			m = m.enterWork(filepath.Join(m.work.root, dir))
		}
		// The file itself, rather than the directory it happens to sit in.
		for i, e := range m.work.shown {
			if e.Name == filepath.Base(hit.Name) {
				m.work.cursor = i
				break
			}
		}
		m.clampWork()
		return m
	}
	m.search.err = fmt.Errorf("conversation %s is no longer indexed", shortID(hit.LaneID))
	return m
}

// filterPrompt is what a list screen shows while its filter is taking keys.
// An active field that looks like an inactive one is how every keystroke ends
// up somewhere the person did not intend.
func (m Model) filterPrompt(f filterInput) string {
	if !f.active {
		return ""
	}
	return m.theme.Accent.Render("filter: ") + m.theme.Value.Render(f.text) +
		m.theme.Accent.Render("▏") + m.theme.Label.Render("  enter keeps it · esc clears")
}

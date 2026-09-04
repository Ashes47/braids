package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// The chrome is modelled on k9s: a compact facts block top-left, key hints
// top-right, and the table inside a titled panel. It exists so a wide terminal
// reads as a dense instrument rather than a mostly-empty list.

// chromeHeight is the lines the chrome occupies: the info block, a blank line,
// the panel's top border, its column header, its bottom border and the status
// line.
func (m Model) chromeHeight() int { return m.headerPlan().rows + 5 }

const (
	// infoLines is the header's natural height; it grows to at most maxInfoLines
	// when a narrow terminal cannot fit every binding in fewer rows.
	infoLines    = 4
	maxInfoLines = 6
	labelCol     = 10
	hintCol      = 8
	glyphCol     = 5
)

type fact struct{ label, value string }
type hint struct{ key, action string }

// glyph is one entry of the visual key. It carries the style the mark is drawn
// with, not a style of its own: a legend in the wrong colour teaches the wrong
// thing, and colour here is meaning rather than decoration.
type glyph struct {
	mark    string
	style   lipgloss.Style
	meaning string
}

func (m Model) facts() []fact {
	source := m.source
	if m.changes != nil {
		source += " · live"
	}
	return []fact{
		{"Source", source},
		{"Index", shorten(m.indexPath)},
		{"Lanes", fmt.Sprintf("%d", len(m.all))},
		{"Waiting on you", fmt.Sprintf("%d", m.waitingCount())},
	}
}

func hints() []hint {
	return []hint{
		// Ordered so that the first few survive a one-column legend on a
		// narrow terminal: moving, opening, searching and quitting.
		{"j/k", "down / up"}, {"↵", "open spine"},
		{"/", "search"}, {"f", "filter list"},
		{"a", "archive"}, {"q", "quit"},
		{"n / N", "next / prev waiting"}, {"d", "delete"},
		{"u", "undo delete"}, {"A", "show archived"},
		{"y", "copy resume"}, {"o", "open terminal"},
	}
}

// info renders the map's facts block, key hints and glyph key.
func (m Model) info() string { return m.factsBlock(m.facts(), hints(), m.mapGlyphs()) }

// mapGlyphs explains the marks the map draws, each in the style it is drawn in.
func (m Model) mapGlyphs() []glyph {
	g := m.theme.Glyphs
	return []glyph{
		{g.Lane, m.theme.Alive, "live conversation"},
		{g.Lane, m.theme.Accent, "waiting on you"},
		{g.Lane, m.theme.Faint, "idle"},
		{g.Archived, m.theme.Faint, "archived"},
		{g.Branch, m.theme.Rail, "branched from above"},
	}
}

// archivedNote tells the user what state the map is in when it is hiding
// things, since a map that silently omits conversations is a map you distrust.
func (m Model) archivedNote() string {
	hidden := 0
	for id := range m.archived {
		if _, ok := m.forestHas[id]; ok {
			hidden++
		}
	}
	switch {
	case m.showArchived && hidden > 0:
		return fmt.Sprintf(" · showing %d archived", hidden)
	case hidden > 0:
		return fmt.Sprintf(" · %d archived hidden", hidden)
	default:
		return ""
	}
}

// factsBlock renders labelled facts on the left and key hints on the right. The
// hint block is padded to a fixed width before being pushed right, so the keys
// form a clean column instead of a ragged edge.
// plan is how the header will be laid out for the current screen and width.
type plan struct {
	rows, columns                     int
	labelWidth, hintWidth, glyphWidth int
	showGlyphs                        bool
}

// headerPlan sizes the header for whichever screen is showing. It is computed
// the same way for drawing and for measuring, so the body can never be given
// room the header has already taken.
func (m Model) headerPlan() plan {
	facts, keys, glyphs := m.headerContent()

	p := plan{labelWidth: labelCol, hintWidth: hintCol}
	factWidth := 0
	for _, f := range facts {
		p.labelWidth = max(p.labelWidth, lipgloss.Width(f.label)+2)
	}
	for _, f := range facts {
		factWidth = max(factWidth, p.labelWidth+lipgloss.Width(f.value))
	}
	for _, k := range keys {
		p.hintWidth = max(p.hintWidth, hintCol+lipgloss.Width(k.action))
	}
	for _, g := range glyphs {
		p.glyphWidth = max(p.glyphWidth, glyphCol+lipgloss.Width(g.meaning))
	}

	p.columns, p.showGlyphs = m.fitColumns(factWidth, p.hintWidth, p.glyphWidth, len(keys), len(glyphs) > 0)
	p.rows = len(keys)
	if p.columns > 1 {
		p.rows = (len(keys) + p.columns - 1) / p.columns
	}
	// Grow rather than drop a binding: a key that works but is not listed may
	// as well not exist. Beyond maxInfoLines the header would cost more of the
	// screen than the legend is worth.
	p.rows = min(max(p.rows, len(facts), len(glyphs), infoLines), maxInfoLines)
	return p
}

func (m Model) factsBlock(facts []fact, keys []hint, glyphs []glyph) string {
	p := m.headerPlan()
	labelWidth, hintWidth, glyphWidth := p.labelWidth, p.hintWidth, p.glyphWidth
	columns, showGlyphs, rows := p.columns, p.showGlyphs, p.rows

	lines := make([]string, 0, rows)
	for i := range rows {
		left := ""
		if i < len(facts) {
			left = m.theme.Label.Render(padRight(facts[i].label+":", labelWidth)) +
				m.theme.Value.Render(facts[i].value)
		}
		right := ""
		for c := range columns {
			if c > 0 {
				right += " "
			}
			if i < rows {
				right += m.hintCell(keys, i+c*rows, hintWidth)
			} else {
				right += strings.Repeat(" ", hintWidth)
			}
		}
		if showGlyphs {
			right = m.glyphCell(glyphs, i, glyphWidth) + "  " + right
		}
		lines = append(lines, " "+spread(left, right, m.width-2))
	}
	return strings.Join(lines, "\n")
}

// headerContent is what the current screen puts in its header.
func (m Model) headerContent() ([]fact, []hint, []glyph) {
	switch {
	case m.mode == searchMode && m.search != nil:
		return m.searchFacts(), searchHints(), nil
	case m.mode == spineMode && m.spine != nil:
		return m.spineFacts(), spineHints(), m.spineGlyphs()
	default:
		return m.facts(), hints(), m.mapGlyphs()
	}
}

// fitColumns decides how much of the header the terminal can hold: as many
// columns of keys as fit, then the glyph key if there is still room.
//
// Every binding a screen has belongs in its legend — a key that works but is
// not listed may as well not exist — so width is spent on columns of keys
// before anything else.
func (m Model) fitColumns(factWidth, hintWidth, glyphWidth, keys int, haveGlyphs bool) (columns int, glyphs bool) {
	room := m.width - 3 // a space either side of the gap, and the margin
	fits := func(width int) bool { return factWidth+width <= room }
	colsWidth := func(n int) int { return hintWidth*n + (n - 1) }

	most := min((keys+infoLines-1)/infoLines, 3)
	// Columns needed to list every binding within the header's maximum height.
	// Listing them all comes first: a key that works but is not shown may as
	// well not exist, while the glyph key only names what is already on screen.
	need := max((keys+maxInfoLines-1)/maxInfoLines, 1)

	for n := most; n >= need && haveGlyphs; n-- {
		if fits(colsWidth(n) + 2 + glyphWidth) {
			return n, true
		}
	}
	for n := most; n >= need; n-- {
		if fits(colsWidth(n)) {
			return n, false
		}
	}
	// Not enough room for every binding: keep as many as will fit, and drop the
	// glyph key before dropping a key.
	for n := need - 1; n >= 1; n-- {
		if fits(colsWidth(n)) {
			return n, false
		}
	}
	return 0, haveGlyphs && fits(glyphWidth)
}

// glyphCell renders one line of the glyph key: the mark in its own style, then
// what it means.
func (m Model) glyphCell(glyphs []glyph, i, width int) string {
	if i >= len(glyphs) {
		return strings.Repeat(" ", width)
	}
	g := glyphs[i]
	return g.style.Render(padRight(g.mark, glyphCol)) +
		m.theme.Label.Render(padRight(g.meaning, width-glyphCol))
}

func (m Model) hintCell(keys []hint, i, width int) string {
	if i >= len(keys) {
		return strings.Repeat(" ", width)
	}
	return m.theme.Column.Render(padRight("<"+keys[i].key+">", hintCol)) +
		m.theme.Label.Render(padRight(keys[i].action, width-hintCol))
}

// panelTitle names what the table is showing, k9s style: Conversations(all)[21].
func (m Model) panelTitle() string {
	scope := "all"
	if m.filter.on() {
		scope = m.filter.label()
	}
	// A map that silently omits conversations is a map you stop trusting, so
	// the title always says whether anything is being held back.
	return fmt.Sprintf("Conversations(%s)[%d]%s", scope, len(m.visible), m.archivedNote())
}

func (m Model) panelTop() string { return m.panelTopTitled(m.panelTitle()) }

func (m Model) panelTopTitled(name string) string {
	title := " " + name + " "
	rule := m.width - 4 - lipgloss.Width(title)
	if rule < 0 {
		rule = 0
	}
	return m.theme.Border.Render("╭─") + m.theme.Panel.Render(title) +
		m.theme.Border.Render(strings.Repeat("─", rule)+"╮")
}

func (m Model) panelBottom() string {
	return m.theme.Border.Render("╰" + strings.Repeat("─", m.width-2) + "╯")
}

// framed puts one already-styled line of the given content width inside the
// panel's vertical borders.
func (m Model) framed(content string) string {
	edge := m.theme.Border.Render("│")
	return edge + content + edge
}

// statusLine is the bottom line: what is being typed, or what is selected.
func (m Model) statusLine() string {
	if m.filter.active {
		return m.typingLine(m.filter)
	}
	if m.notice != "" {
		return " " + m.noticeStyle(m.failed).Render(truncate(m.notice, m.width-2))
	}
	if len(m.visible) == 0 {
		return ""
	}
	return " " + m.theme.Label.Render(m.visible[m.cursor].node.Lane.ID)
}

// noticeStyle distinguishes an outcome that worked from one that did not.
func (m Model) noticeStyle(failed bool) lipgloss.Style {
	if failed {
		return m.theme.Column
	}
	return m.theme.Alive
}

// typingLine shows the filter being typed, on either screen.
func (m Model) typingLine(f filterInput) string {
	return " " + m.theme.Column.Render("/") + m.theme.Value.Render(f.text) +
		m.theme.Column.Render("▏") + "  " + m.theme.Label.Render("enter keep · esc clear")
}

// shorten replaces the home directory with ~ so a path fits the facts block.
func shorten(path string) string {
	if home, err := homeDir(); err == nil && home != "" && strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/Ashes47/braids/internal/brand"
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
	maxInfoLines = 7
	labelCol     = 10
	hintCol      = 8
	glyphCol     = 5
	// minFrameWidth and minFrameHeight are the smallest frame the layout is
	// defined for. A terminal can report less — a one-column pane, or a resize
	// caught mid-flight — and the arithmetic here subtracts borders and
	// margins, so below the floor those subtractions go negative and the frame
	// panics rather than merely looking bad. Clamping draws something too wide
	// for a window that could not have held it anyway.
	minFrameWidth  = 24
	minFrameHeight = 8
	// The glyph key and the keys are two legends doing different jobs — one
	// names what is on screen, the other what you can press — and they read as
	// a single list when set too close. So they are pushed apart, but only
	// using room nothing else wants: on a narrow terminal the gap closes back
	// to its minimum rather than costing a legend entry.
	minGlyphGap = 2
	maxGlyphGap = 6
	// factsGap separates the facts column from the legend beside it.
	factsGap = 4
	// logoGap is the least slack left before the mark when deciding whether it
	// fits. The mark is flush right, so the space actually shown is whatever
	// the legend does not use; widening glyphGap is what closes it up.
	logoGap = 2
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
	// Said plainly, because it decides how much the state column can know:
	// with hooks a session reports that it is blocked, without them braids
	// infers from the transcript and cannot tell working from waiting.
	reporting := "off · see braids hooks"
	if m.reporting {
		reporting = "reporting"
	}
	return []fact{
		{"Source", source},
		{"Index", shorten(m.indexPath)},
		{"Lanes", fmt.Sprintf("%d", len(m.all))},
		{"Waiting on you", fmt.Sprintf("%d", m.waitingCount())},
		{"Hooks", reporting},
	}
}

func hints() []hint {
	return []hint{
		// Ordered so that the first few survive a one-column legend on a
		// narrow terminal: moving, opening, searching and quitting.
		{"j/k", "down / up"}, {"↵", "open spine"},
		{"/", "search"}, {"f", "filter list"},
		{"a", "toggle archive"}, {"r", "rename"},
		{"q", "quit"},
		{"n / N", "next / prev waiting"}, {"d", "delete"},
		{"D", "delete work products"},
		{"u", "deleted / recover"}, {"A", "show archived"},
		{"y", "copy resume"}, {"o", "open terminal"},
	}
}

// info renders the map's facts block, key hints and glyph key.
func (m Model) info() string { return m.factsBlock(m.facts(), hints(), m.mapGlyphs()) }

// mapGlyphs explains the marks the map draws, each in the style it is drawn in.
func (m Model) mapGlyphs() []glyph {
	g := m.theme.Glyphs
	return []glyph{
		{g.Needs, m.theme.Urgent, "stopped, needs you"},
		{g.Lane, m.theme.Alive, "working"},
		{g.Lane, m.theme.Accent, "an open loop"},
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
	factWidth, glyphGap               int
	showGlyphs                        bool
	// logo is the mark the header has room for, largest first, or nil when
	// even the small one would crowd the legend.
	logo []string
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

	p.factWidth = factWidth
	// The mark is decoration and is priced accordingly. It is taken before the
	// legend spreads into extra columns, and only ever after both legends are
	// guaranteed a place: a key that works and is not listed may as well not
	// exist, and the glyph key names marks that are on the screen right now,
	// while the mark names a tool you are plainly already running.
	reserved := 0
	minimum := factWidth + columnsWidth(p.hintWidth, legendColumns(len(keys)))
	if len(glyphs) > 0 {
		minimum += p.glyphWidth + minGlyphGap
	}
	for _, art := range logoSizes() {
		if minimum+logoGap+brand.Width(art) <= m.width-3 {
			p.logo, reserved = art, logoGap+brand.Width(art)
			break
		}
	}
	p.columns, p.showGlyphs = m.fitColumns(factWidth+reserved, p.hintWidth, p.glyphWidth, len(keys), len(glyphs) > 0)

	// Whatever is still unspent goes into separating the two legends, up to
	// the point where more space stops helping.
	p.glyphGap = minGlyphGap
	if p.showGlyphs {
		used := factWidth + reserved + columnsWidth(p.hintWidth, p.columns) + p.glyphWidth + minGlyphGap
		p.glyphGap += max(0, min(m.width-3-used, maxGlyphGap-minGlyphGap))
	}
	p.rows = len(keys)
	if p.columns > 1 {
		p.rows = (len(keys) + p.columns - 1) / p.columns
	}
	// Grow rather than drop a binding: a key that works but is not listed may
	// as well not exist. Beyond maxInfoLines the header would cost more of the
	// screen than the legend is worth.
	p.rows = min(max(p.rows, len(facts), len(glyphs), infoLines), maxInfoLines)
	if p.logo != nil {
		p.rows = max(p.rows, len(p.logo))
	}
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
				right += repeat(" ", hintWidth)
			}
		}
		if showGlyphs {
			right = m.glyphCell(glyphs, i, glyphWidth) + repeat(" ", p.glyphGap) + right
		}
		if p.logo == nil {
			lines = append(lines, " "+spread(left, right, m.width-2))
			continue
		}
		// With the mark present the facts can no longer float: they are padded
		// to their own column so the legend keeps a straight left edge between
		// them and the mark.
		mark := ""
		if i < len(p.logo) {
			mark = m.theme.Logo.Render(p.logo[i])
		}
		body := padRight(left, p.factWidth) + repeat(" ", factsGap) + right
		lines = append(lines, " "+spread(body, mark, m.width-2))
	}
	return strings.Join(lines, "\n")
}

// headerContent is what the current screen puts in its header.
func (m Model) headerContent() ([]fact, []hint, []glyph) {
	switch {
	case m.mode == searchMode && m.search != nil:
		return m.searchFacts(), searchHints(), nil
	case m.mode == binMode && m.bin != nil:
		return m.binFacts(), binHints(), nil
	case m.mode == spineMode && m.spine != nil:
		return m.spineFacts(), m.spineHints(), m.spineGlyphs()
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
	colsWidth := func(n int) int { return columnsWidth(hintWidth, n) }

	most := min((keys+infoLines-1)/infoLines, 3)
	// Columns needed to list every binding within the header's maximum height.
	// Listing them all comes first: a key that works but is not shown may as
	// well not exist, while the glyph key only names what is already on screen.
	need := legendColumns(keys)

	for n := most; n >= need && haveGlyphs; n-- {
		if fits(colsWidth(n) + minGlyphGap + glyphWidth) {
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

// legendColumns is the fewest columns that can list every binding inside the
// header's maximum height.
func legendColumns(keys int) int {
	return max((keys+maxInfoLines-1)/maxInfoLines, 1)
}

// columnsWidth is what n columns of the legend occupy, gaps included.
func columnsWidth(hintWidth, n int) int {
	if n <= 0 {
		return 0
	}
	return hintWidth*n + (n - 1)
}

// glyphCell renders one line of the glyph key: the mark in its own style, then
// what it means.
func (m Model) glyphCell(glyphs []glyph, i, width int) string {
	if i >= len(glyphs) {
		return repeat(" ", width)
	}
	g := glyphs[i]
	return g.style.Render(padRight(g.mark, glyphCol)) +
		m.theme.Label.Render(padRight(g.meaning, width-glyphCol))
}

func (m Model) hintCell(keys []hint, i, width int) string {
	if i >= len(keys) {
		return repeat(" ", width)
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
	// "╭─" and the closing "╮" are three columns, not four: the rule fills
	// what is left of the width after them and the title.
	rule := m.width - 3 - lipgloss.Width(title)
	if rule < 0 {
		rule = 0
	}
	return m.theme.Border.Render("╭─") + m.theme.Panel.Render(title) +
		m.theme.Border.Render(repeat("─", rule)+"╮")
}

func (m Model) panelBottom() string {
	return m.theme.Border.Render("╰" + repeat("─", m.width-2) + "╯")
}

// framed puts one already-styled line of the given content width inside the
// panel's vertical borders.
func (m Model) framed(content string) string {
	edge := m.theme.Border.Render("│")
	return edge + content + edge
}

// statusLine is the bottom line: what is being typed, or what is selected.
func (m Model) statusLine() string {
	if m.naming.active {
		return " " + m.theme.Label.Render("renaming — enter saves, esc cancels")
	}
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

// repeat pads by n columns, treating a negative count as none. Width
// arithmetic subtracts borders, gaps and margins; one of those going negative
// on an unusually small frame should draw a short line, never bring the
// program down.
func repeat(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(s, n)
}

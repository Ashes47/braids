package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// The chrome is modelled on k9s: a compact facts block top-left, key hints
// top-right, and the table inside a titled panel. It exists so a wide terminal
// reads as a dense instrument rather than a mostly-empty list.

// chromeHeight is the number of lines the chrome occupies: the info block, a
// blank line, the panel's top border, its column header, its bottom border and
// the status line.
const chromeHeight = infoLines + 1 + 1 + 1 + 1 + 1

const (
	infoLines = 4
	labelCol  = 10
	hintCol   = 8
	glyphCol  = 5
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
		{"j/k", "down / up"}, {"↵", "open spine"},
		{"n / N", "next / prev waiting"}, {"/", "search"},
		{"f", "filter"}, {"y / o", "copy / open"},
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
		{g.Branch, m.theme.Rail, "branched from above"},
	}
}

// factsBlock renders labelled facts on the left and key hints on the right. The
// hint block is padded to a fixed width before being pushed right, so the keys
// form a clean column instead of a ragged edge.
func (m Model) factsBlock(facts []fact, keys []hint, glyphs []glyph) string {
	labelWidth := labelCol
	for _, f := range facts {
		labelWidth = max(labelWidth, lipgloss.Width(f.label)+2)
	}
	factWidth := 0
	for _, f := range facts {
		factWidth = max(factWidth, labelWidth+lipgloss.Width(f.value))
	}
	hintWidth := hintCol
	for _, k := range keys {
		hintWidth = max(hintWidth, hintCol+lipgloss.Width(k.action))
	}
	glyphWidth := 0
	for _, g := range glyphs {
		glyphWidth = max(glyphWidth, glyphCol+lipgloss.Width(g.meaning))
	}

	// Choose the widest layout that fits, dropping columns rather than letting
	// the header spill past the edge of the terminal.
	columns, showGlyphs := m.fitColumns(factWidth, hintWidth, glyphWidth, len(keys) > 0, len(glyphs) > 0)
	rows := len(keys)
	if columns == 2 {
		rows = (len(keys) + 1) / 2
	}
	rows = min(max(rows, 1), infoLines)

	lines := make([]string, 0, infoLines)
	for i := range infoLines {
		left := ""
		if i < len(facts) {
			left = m.theme.Label.Render(padRight(facts[i].label+":", labelWidth)) +
				m.theme.Value.Render(facts[i].value)
		}
		right := ""
		switch {
		case columns == 2 && i < rows:
			right = m.hintCell(keys, i, hintWidth) + " " + m.hintCell(keys, i+rows, hintWidth)
		case columns == 2:
			right = strings.Repeat(" ", hintWidth*2+1)
		case columns == 1:
			right = m.hintCell(keys, i, hintWidth)
		}
		if showGlyphs {
			right = m.glyphCell(glyphs, i, glyphWidth) + "  " + right
		}
		lines = append(lines, " "+spread(left, right, m.width-2))
	}
	return strings.Join(lines, "\n")
}

// fitColumns decides how much of the header the terminal can hold.
func (m Model) fitColumns(factWidth, hintWidth, glyphWidth int, haveHints, haveGlyphs bool) (columns int, glyphs bool) {
	room := m.width - 3 // one space either side of the gap, one for the margin
	fits := func(width int) bool { return factWidth+width <= room }

	if !haveHints {
		return 0, haveGlyphs && fits(glyphWidth)
	}
	twoCols := hintWidth*2 + 1
	switch {
	case haveGlyphs && fits(glyphWidth+2+twoCols):
		return 2, true
	case fits(twoCols):
		return 2, false
	case fits(hintWidth):
		return 1, false
	default:
		return 0, false
	}
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
	return fmt.Sprintf("Conversations(%s)[%d]", scope, len(m.visible))
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

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
)

type fact struct{ label, value string }
type hint struct{ key, action string }

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
		{"j/k", "move"}, {"↵", "open spine"},
		{"n/N", "next waiting"}, {"/", "search"},
		{"f", "filter"}, {"y/o", "copy / open"},
	}
}

// info renders the map's facts block and key hints.
func (m Model) info() string { return m.factsBlock(m.facts(), hints()) }

// factsBlock renders labelled facts on the left and key hints on the right. The
// hint block is padded to a fixed width before being pushed right, so the keys
// form a clean column instead of a ragged edge.
func (m Model) factsBlock(facts []fact, keys []hint) string {
	hintWidth := hintCol
	for _, k := range keys {
		hintWidth = max(hintWidth, hintCol+lipgloss.Width(k.action))
	}
	labelWidth := labelCol
	for _, f := range facts {
		labelWidth = max(labelWidth, lipgloss.Width(f.label)+2)
	}
	// Two columns of hints: one column cannot hold everything a screen binds,
	// and a legend that omits a working key is worse than no legend.
	rows := (len(keys) + 1) / 2
	if rows > infoLines {
		rows = infoLines
	}

	lines := make([]string, 0, infoLines)
	for i := range infoLines {
		left := ""
		if i < len(facts) {
			left = m.theme.Label.Render(padRight(facts[i].label+":", labelWidth)) +
				m.theme.Value.Render(facts[i].value)
		}
		right := strings.Repeat(" ", hintWidth*2+1)
		if i < rows {
			right = m.hintCell(keys, i, hintWidth) + " " + m.hintCell(keys, i+rows, hintWidth)
		}
		lines = append(lines, " "+spread(left, right, m.width-2))
	}
	return strings.Join(lines, "\n")
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

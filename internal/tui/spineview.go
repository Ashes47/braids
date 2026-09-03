package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/Ashes47/braids/internal/core/graph"
	"github.com/Ashes47/braids/internal/core/index"
	"github.com/Ashes47/braids/internal/core/model"
)

// spineState is one conversation opened for reading.
type spineState struct {
	lane    index.LaneInfo
	segs    []graph.Segment
	visible []graph.Segment
	filter  filterInput
	cursor  int
	offset  int
	err     error
}

// apply narrows the spine to segments matching the filter. A run is matched on
// its summary, so filtering for a tool finds the stretches that used it.
func (s *spineState) apply() {
	if !s.filter.on() {
		s.visible = s.segs
		return
	}
	s.visible = nil
	for _, seg := range s.segs {
		hay := fmt.Sprintf("t%d %s %s %s %s",
			seg.Seq, seg.Role, seg.Preview, strings.Join(seg.Tools, " "), summarise(seg))
		if s.filter.matches(hay) {
			s.visible = append(s.visible, seg)
		}
	}
}

// openSpine loads the selected lane. Failure is shown in place rather than
// thrown away, because a lane that cannot be read is itself worth seeing.
func (m Model) openSpine() Model {
	if len(m.visible) == 0 || m.loadSpine == nil {
		return m
	}
	lane := m.visible[m.cursor].node.Lane
	segs, err := m.loadSpine(lane.ID)
	m.spine = &spineState{lane: lane, segs: segs, err: err}
	m.spine.apply()
	m.mode = spineMode
	return m
}

func (m Model) spineKey(key string) Model {
	s := m.spine
	if s.filter.key(key) {
		s.apply()
		m.clampSpine()
		return m
	}
	switch key {
	case "esc", "backspace", "h", "left":
		m.mode = mapMode
		m.spine = nil
		return m
	case "j", "down":
		s.cursor++
	case "k", "up":
		s.cursor--
	case "g", "home":
		s.cursor = 0
	case "G", "end":
		s.cursor = len(s.visible) - 1
	case "ctrl+d", "pgdown":
		s.cursor += m.bodyHeight() / 2
	case "ctrl+u", "pgup":
		s.cursor -= m.bodyHeight() / 2
	case "n":
		s.cursor = nextJunction(s.visible, s.cursor, 1)
	case "N":
		s.cursor = nextJunction(s.visible, s.cursor, -1)
	}
	m.clampSpine()
	return m
}

func (m *Model) clampSpine() {
	s := m.spine
	if s == nil {
		return
	}
	if s.cursor >= len(s.visible) {
		s.cursor = len(s.visible) - 1
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

func (m Model) renderSpine() string {
	s := m.spine
	var b strings.Builder
	b.WriteString(m.spineInfo())
	b.WriteString("\n\n")
	b.WriteString(m.panelTopTitled(m.spineTitle()))
	b.WriteString("\n")
	b.WriteString(m.framed(m.spineColumns()))
	b.WriteString("\n")

	blank := strings.Repeat(" ", m.contentWidth())
	switch {
	case s.err != nil:
		b.WriteString(m.framed(padRight(" "+m.theme.Empty.Render(s.err.Error()), m.contentWidth())))
		b.WriteString("\n")
		for range m.bodyHeight() - 1 {
			b.WriteString(m.framed(blank) + "\n")
		}
	case len(s.visible) == 0:
		b.WriteString(m.framed(padRight(" "+m.theme.Empty.Render(m.spineEmptyMessage()), m.contentWidth())))
		b.WriteString("\n")
		for range m.bodyHeight() - 1 {
			b.WriteString(m.framed(blank) + "\n")
		}
	default:
		end := min(s.offset+m.bodyHeight(), len(s.visible))
		for i := s.offset; i < end; i++ {
			b.WriteString(m.framed(m.renderSegment(s.visible[i], i == s.cursor)))
			b.WriteString("\n")
		}
		for range m.bodyHeight() - (end - s.offset) {
			b.WriteString(m.framed(blank) + "\n")
		}
	}
	b.WriteString(m.panelBottom())
	b.WriteString("\n")
	if s.filter.active {
		b.WriteString(m.typingLine(s.filter))
	} else {
		b.WriteString(" " + m.theme.Label.Render(s.lane.ID))
	}
	return b.String()
}

func (m Model) spineEmptyMessage() string {
	if m.spine.filter.on() {
		return fmt.Sprintf("nothing matches %q", m.spine.filter.text)
	}
	return "this conversation has no turns"
}

func (m Model) spineTitle() string {
	name := m.spine.lane.Title
	if name == "" {
		name = shortID(m.spine.lane.ID)
	}
	scope := ""
	if m.spine.filter.on() {
		scope = "(" + m.spine.filter.label() + ")"
	}
	return fmt.Sprintf("Spine(%s)%s[%d]", truncate(name, 40), scope, len(m.spine.visible))
}

// spineInfo swaps the map's facts for the ones that matter inside a lane.
func (m Model) spineInfo() string {
	junctions, alternates := 0, 0
	for _, seg := range m.spine.segs {
		if len(seg.Alternates) > 0 {
			junctions++
			alternates += len(seg.Alternates)
		}
	}
	facts := []fact{
		{"Lane", shortID(m.spine.lane.ID)},
		{"Turns", fmt.Sprintf("%d", m.spine.lane.Messages)},
		{"Junctions", fmt.Sprintf("%d", junctions)},
		{"Branches", fmt.Sprintf("%d", alternates)},
	}
	keys := []hint{
		{"j/k", "move"},
		{"/", "filter"},
		{"n/N", "next junction"},
		{"esc", "back to map"},
	}
	return m.factsBlock(facts, keys)
}

func (m Model) spineColumns() string {
	right := padLeft("BRANCHES", 10)
	nameWidth := m.contentWidth() - 2 - lipgloss.Width(right)
	if nameWidth < 8 {
		nameWidth = 8
	}
	return m.theme.Column.Render(" " + padRight(" TURN  WHO     WHAT HAPPENED", nameWidth) + " " + right)
}

const (
	seqWidth      = 5
	whoWidth      = 7
	branchesWidth = 10
)

func (m Model) renderSegment(seg graph.Segment, selected bool) string {
	plain, styled := m.segmentParts(seg)
	if selected {
		return m.theme.Selected.Width(m.contentWidth()).Render(plain)
	}
	return styled
}

func (m Model) segmentParts(seg graph.Segment) (plain, styled string) {
	g := m.theme.Glyphs

	branches := ""
	if n := len(seg.Alternates); n > 0 {
		branches = fmt.Sprintf("%s %d", g.Branch, n)
	}
	branches = padLeft(branches, branchesWidth)

	// " " + mark + " " + seq + " " + who + " " + body + " " + branches
	textWidth := m.contentWidth() - (4 + seqWidth + whoWidth + branchesWidth + 2)
	if textWidth < 8 {
		textWidth = 8
	}

	var mark, who, body string
	var markStyle, bodyStyle lipgloss.Style
	switch seg.Kind {
	case graph.SegRun:
		mark, markStyle = g.Run, m.theme.Faint
		who, body, bodyStyle = "", summarise(seg), m.theme.Faint
	default:
		mark, markStyle = g.Lane, m.theme.Faint
		who, bodyStyle = "claude", m.theme.Dim
		if seg.Role == model.RoleUser {
			who, markStyle, bodyStyle = "you", m.theme.Alive, m.theme.Value
		}
		body = seg.Preview
		if strings.TrimSpace(body) == "" && len(seg.Tools) > 0 {
			body = strings.Join(seg.Tools, ", ")
			bodyStyle = m.theme.Faint
		}
	}
	body = padRight(truncate(oneLine(body), textWidth), textWidth)
	seq := padLeft(fmt.Sprintf("t%d", seg.Seq), seqWidth)
	who = padRight(who, whoWidth)

	plain = " " + mark + " " + seq + " " + who + " " + body + " " + branches
	styled = " " + markStyle.Render(mark) + " " +
		m.theme.Faint.Render(seq) + " " +
		m.theme.Label.Render(who) + " " +
		bodyStyle.Render(body) + " " +
		m.theme.Column.Render(branches)
	return plain, styled
}

// nextJunction finds the next turn that a branch leaves from, wrapping around.
// With hundreds of junctions in a long conversation, stepping between them is
// the only practical way to find where the thread split.
func nextJunction(segs []graph.Segment, from, step int) int {
	for i := 1; i <= len(segs); i++ {
		at := (from + i*step + len(segs)*i) % len(segs)
		if len(segs[at].Alternates) > 0 {
			return at
		}
	}
	return from
}

// summarise describes a collapsed run: how many turns, and what ran inside it.
func summarise(seg graph.Segment) string {
	parts := []string{fmt.Sprintf("%d turns", seg.Count)}
	for _, t := range seg.Tally {
		if len(parts) == 4 {
			break
		}
		parts = append(parts, fmt.Sprintf("%d %s", t.Count, t.Tool))
	}
	return strings.Join(parts, " · ")
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

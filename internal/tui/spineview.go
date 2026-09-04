package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Ashes47/braids/internal/core/graph"
	"github.com/Ashes47/braids/internal/core/index"
	"github.com/Ashes47/braids/internal/core/model"
)

// spineRow is one line of the spine: either a turn, or a lane that forked away
// at this point. Showing forks inline is the whole reason the spine exists —
// otherwise a branch is only visible from the map, and a conversation gives no
// hint that it ever split.
type spineRow struct {
	seg   graph.Segment
	fork  *graph.Node
	agent *index.SubagentRow
}

// spineState is one conversation opened for reading.
type spineState struct {
	lane    index.LaneInfo
	node    *graph.Node
	segs    []graph.Segment
	agents  []index.SubagentRow
	rows    []spineRow
	visible []spineRow
	filter  filterInput
	// naming is the branch-name field, opened with `b` on a turn.
	naming filterInput
	notice string
	failed bool
	cursor int
	offset int
	err    error
}

// build interleaves the lane's turns with the branches that left it, each at the
// turn it forked from.
func (s *spineState) build() {
	children := append([]*graph.Node(nil), childrenOf(s.node)...)
	sort.SliceStable(children, func(i, j int) bool { return children[i].ForkSeq < children[j].ForkSeq })
	agents := append([]index.SubagentRow(nil), s.agents...)
	sort.SliceStable(agents, func(i, j int) bool { return agents[i].ParentSeq < agents[j].ParentSeq })

	s.rows = nil
	fork, agent := 0, 0
	emitUpTo := func(seq int) {
		for agent < len(agents) && agents[agent].ParentSeq <= seq {
			a := agents[agent]
			s.rows = append(s.rows, spineRow{agent: &a})
			agent++
		}
		for fork < len(children) && children[fork].ForkSeq <= seq {
			s.rows = append(s.rows, spineRow{fork: children[fork]})
			fork++
		}
	}
	for _, seg := range s.segs {
		s.rows = append(s.rows, spineRow{seg: seg})
		emitUpTo(seg.Seq)
	}
	emitUpTo(int(^uint(0) >> 1))
	s.apply()
}

// current is the row under the cursor, or a zero row when there is none.
func (s *spineState) current() spineRow {
	if s.cursor < 0 || s.cursor >= len(s.visible) {
		return spineRow{}
	}
	return s.visible[s.cursor]
}

func childrenOf(n *graph.Node) []*graph.Node {
	if n == nil {
		return nil
	}
	return n.Children
}

// apply narrows the spine to rows matching the filter. A run is matched on its
// summary, so filtering for a tool finds the stretches that used it.
//
// The selected row is followed across the change. Filtering down to one turn
// and then clearing the filter should leave you on that turn in the full spine
// — that is the whole reason to filter — and losing the place would mean
// finding it twice.
func (s *spineState) apply() {
	held := s.selectedKey()
	switch {
	case !s.filter.on():
		s.visible = s.rows
	default:
		s.visible = nil
		for _, r := range s.rows {
			if s.filter.matches(r.haystack()) {
				s.visible = append(s.visible, r)
			}
		}
	}
	s.restore(held)
}

// selectedKey identifies the current row so it can be found again.
func (s *spineState) selectedKey() string {
	if s.cursor < 0 || s.cursor >= len(s.visible) {
		return ""
	}
	return rowKey(s.visible[s.cursor])
}

// restore puts the cursor back on a row, or at the top if it is gone.
func (s *spineState) restore(key string) {
	s.cursor = 0
	if key == "" {
		return
	}
	for i, r := range s.visible {
		if rowKey(r) == key {
			s.cursor = i
			return
		}
	}
}

func rowKey(r spineRow) string {
	if r.agent != nil {
		return "agent:" + r.agent.ID
	}
	if r.fork != nil {
		return "fork:" + r.fork.Lane.ID
	}
	return fmt.Sprintf("turn:%d", r.seg.Seq)
}

func (r spineRow) haystack() string {
	if r.agent != nil {
		return r.agent.Type + " " + r.agent.Task + " " + r.agent.ID
	}
	if r.fork != nil {
		return r.fork.Lane.Title + " " + r.fork.Lane.ID
	}
	return fmt.Sprintf("t%d %s %s %s %s",
		r.seg.Seq, r.seg.Role, r.seg.Preview, strings.Join(r.seg.Tools, " "), summarise(r.seg))
}

// openSpine loads the selected lane. Failure is shown in place rather than
// thrown away, because a lane that cannot be read is itself worth seeing.
func (m Model) openSpine() Model {
	if len(m.visible) == 0 || m.loadSpine == nil {
		return m
	}
	return m.openNode(m.visible[m.cursor].node, false)
}

// openNode reads one lane. push keeps the current spine on a stack so that esc
// walks back down the branch you came in through.
func (m Model) openNode(n *graph.Node, push bool) Model {
	if m.loadSpine == nil {
		return m
	}
	segs, err := m.loadSpine(n.Lane.ID)
	next := &spineState{lane: n.Lane, node: n, segs: segs, err: err}
	if m.loadAgents != nil {
		if agents, agentErr := m.loadAgents(n.Lane.ID); agentErr == nil {
			next.agents = agents
		}
	}
	next.build()
	if push && m.spine != nil {
		m.stack = append(m.stack, m.spine)
	}
	m.spine = next
	m.mode = spineMode
	return m
}

// closeSpine steps back: to the lane we descended from, or to the map.
func (m Model) closeSpine() Model {
	if n := len(m.stack); n > 0 {
		m.spine = m.stack[n-1]
		m.stack = m.stack[:n-1]
		return m
	}
	m.mode = mapMode
	m.spine = nil
	return m
}

func (m Model) spineKey(key string) (Model, tea.Cmd) {
	s := m.spine
	if s.naming.active {
		return m.namingKey(key), nil
	}
	if s.filter.key(key) {
		s.apply()
		m.clampSpine()
		return m, nil
	}
	switch key {
	case "esc", "backspace", "h", "left":
		return m.closeSpine(), nil
	case "p":
		return m.promoteAgent(), nil
	case "enter", "l", "right":
		row := s.current()
		if row.agent != nil {
			s.notice, s.failed = "press p to promote this agent into a conversation you can open", true
			return m, nil
		}
		if row.fork == nil {
			s.notice, s.failed = "no branch on this line — press b to make one, or n to find the next split", true
			return m, nil
		}
		return m.openNode(row.fork, true), nil
	case "j", "down":
		s.cursor = wrap(s.cursor, 1, len(s.visible))
	case "k", "up":
		s.cursor = wrap(s.cursor, -1, len(s.visible))
	case "g", "home":
		s.cursor = 0
	case "G", "end":
		s.cursor = len(s.visible) - 1
	case "ctrl+d", "pgdown":
		s.cursor += m.bodyHeight() / 2
	case "ctrl+u", "pgup":
		s.cursor -= m.bodyHeight() / 2
	case "f":
		s.filter.active = true
	case "b":
		return m.startBranch(), nil
	case "y":
		updated, cmd := m.copyResume()
		return updated.(Model), cmd
	case "o":
		updated, cmd := m.openTerminal()
		return updated.(Model), cmd
	case "n":
		s.cursor = nextMarker(s.visible, s.cursor, 1)
	case "N":
		s.cursor = nextMarker(s.visible, s.cursor, -1)
	}
	m.clampSpine()
	return m, nil
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
		drawn := 0
		end := min(s.offset+m.bodyHeight(), len(s.visible))
		for i := s.offset; i < end && drawn < m.bodyHeight(); i++ {
			b.WriteString(m.framed(m.renderRowLine(s.visible[i], i == s.cursor)))
			b.WriteString("\n")
			drawn++
			// The name field appears where the branch will be cut, not in some
			// corner of the screen: the point of the action is the turn.
			if s.naming.active && i == s.cursor && drawn < m.bodyHeight() {
				b.WriteString(m.framed(m.namePrompt()))
				b.WriteString("\n")
				drawn++
			}
		}
		for range m.bodyHeight() - drawn {
			b.WriteString(m.framed(blank) + "\n")
		}
	}
	b.WriteString(m.panelBottom())
	b.WriteString("\n")
	b.WriteString(m.spineStatus())
	return b.String()
}

// namePrompt is the inline branch-name field, drawn under the turn it will cut.
func (m Model) namePrompt() string {
	g := m.theme.Glyphs
	label := "branch from t" + fmt.Sprintf("%d", m.spine.current().seg.Seq) + ": "
	hint := "enter create · esc cancel"
	field := m.spine.naming.text

	width := m.contentWidth() - 4 - lipgloss.Width(g.Last) - lipgloss.Width(label) -
		lipgloss.Width(hint) - 2
	if width < 8 {
		width = 8
	}
	return " " + m.theme.Rail.Render("  "+g.Last) + " " +
		m.theme.Accent.Render(label) +
		m.theme.Value.Render(padRight(truncate(field, width)+"▏", width+1)) + " " +
		m.theme.Label.Render(hint)
}

// spineStatus is the bottom line: the field being typed, the last outcome, or
// the lane's identity.
func (m Model) spineStatus() string {
	s := m.spine
	switch {
	case s.filter.active:
		return m.typingLine(s.filter)
	case s.notice != "":
		style := m.theme.Alive
		if s.failed {
			style = m.theme.Column
		}
		return " " + style.Render(s.notice)
	default:
		return " " + m.theme.Label.Render(s.lane.ID)
	}
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
	forks := len(childrenOf(m.spine.node))
	agents := len(m.spine.agents)
	facts := []fact{
		{"Lane", shortID(m.spine.lane.ID)},
		{"Turns", fmt.Sprintf("%d", m.spine.lane.Messages)},
		{"Branches", describeBranches(alternates, forks)},
		{"Agents", fmt.Sprintf("%d", agents)},
	}
	keys := []hint{
		{"j/k", "move"}, {"b", "branch here"},
		{"p", "promote agent"}, {"/", "search"},
		{"↵", "open branch"}, {"n/N", "next marker"},
		{"y/o", "copy / open"}, {"esc", "back"},
	}
	return m.factsBlock(facts, keys)
}

// describeBranches names the two kinds of branch a conversation can have, since
// a bare "0 / 0" says nothing about what is being counted.
//
//	kept here  — an alternative still inside this transcript, left behind by a
//	             rewind or an edited message. Claude Code cannot show these.
//	forked out — a branch that became a conversation of its own.
func describeBranches(kept, forked int) string {
	switch {
	case kept == 0 && forked == 0:
		return "none"
	case forked == 0:
		return fmt.Sprintf("%d kept here", kept)
	case kept == 0:
		return fmt.Sprintf("%d forked out", forked)
	default:
		return fmt.Sprintf("%d kept here · %d forked out", kept, forked)
	}
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

func (m Model) renderRowLine(row spineRow, selected bool) string {
	var plain, styled string
	switch {
	case row.agent != nil:
		plain, styled = m.agentParts(row.agent)
	case row.fork != nil:
		plain, styled = m.forkParts(row.fork)
	default:
		plain, styled = m.segmentParts(row.seg)
	}
	if selected {
		return m.theme.Selected.Width(m.contentWidth()).Render(plain)
	}
	return styled
}

// agentParts draws a conversation the lane spawned and then showed as a single
// tool call. Claude Code gives no way to see these at all.
func (m Model) agentParts(a *index.SubagentRow) (plain, styled string) {
	g := m.theme.Glyphs
	lead := "  " + g.Branch + g.Agent + " "
	right := padLeft(fmt.Sprintf("%d turns", a.Messages), branchesWidth)

	label := a.Type
	if a.Task != "" {
		label += " · " + a.Task
	}
	textWidth := m.contentWidth() - lipgloss.Width(lead) - 1 - branchesWidth - 1
	if textWidth < 8 {
		textWidth = 8
	}
	body := padRight(truncate(label, textWidth), textWidth)

	plain = " " + lead + body + " " + right
	styled = " " + m.theme.Rail.Render("  "+g.Branch) + m.theme.Accent.Render(g.Agent) + " " +
		m.theme.Dim.Render(body) + " " + m.theme.Faint.Render(right)
	return plain, styled
}

// forkParts draws a branch that left this conversation, indented under the turn
// it left from so the split reads as part of the thread.
func (m Model) forkParts(n *graph.Node) (plain, styled string) {
	g := m.theme.Glyphs
	name := n.Lane.Title
	if name == "" {
		name = shortID(n.Lane.ID)
	}
	lead := "  " + g.Last + g.Lane + " "
	right := padLeft(fmt.Sprintf("%s t%d", g.Fork, n.ForkSeq), branchesWidth)

	textWidth := m.contentWidth() - lipgloss.Width(lead) - 1 - branchesWidth - 1
	if textWidth < 8 {
		textWidth = 8
	}
	body := padRight(truncate(name+fmt.Sprintf("  (%d turns)", n.Lane.Messages), textWidth), textWidth)

	plain = " " + lead + body + " " + right
	styled = " " + m.theme.Rail.Render("  "+g.Last) + m.theme.Alive.Render(g.Lane) + " " +
		m.theme.Value.Render(body) + " " + m.theme.Column.Render(right)
	return plain, styled
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

// startBranch opens the name field, pre-filled from the turn being branched.
func (m Model) startBranch() Model {
	s := m.spine
	if m.branch == nil || len(s.visible) == 0 {
		s.notice, s.failed = "branching is unavailable for this source", true
		return m
	}
	row := s.current()
	if row.fork != nil {
		s.notice, s.failed = "press enter to open that branch, or branch from a turn instead", true
		return m
	}
	seg := row.seg
	if seg.Kind == graph.SegRun {
		s.notice, s.failed = "pick a single turn to branch from, not a collapsed run", true
		return m
	}
	s.naming = filterInput{active: true, text: suggestName(seg)}
	s.notice, s.failed = "", false
	return m
}

func (m Model) namingKey(key string) Model {
	s := m.spine
	switch key {
	case "esc":
		s.naming = filterInput{}
	case "enter":
		return m.commitBranch()
	default:
		s.naming.edit(key)
	}
	return m
}

// commitBranch writes the new lane, then refreshes so it appears immediately.
// Only what changed is re-read, so this costs milliseconds rather than the
// seconds a full rebuild takes.
func (m Model) commitBranch() Model {
	s := m.spine
	seg := s.current().seg
	name := strings.TrimSpace(s.naming.text)
	s.naming = filterInput{}

	id, err := m.branch(s.lane.ID, seg.Seq, name)
	if err != nil {
		s.notice, s.failed = err.Error(), true
		return m
	}
	notice := fmt.Sprintf("branched at t%d → %s", seg.Seq, shortID(id))
	if m.refresh != nil {
		forest, err := m.refresh()
		if err != nil {
			s.notice, s.failed = notice+" · but the refresh failed: "+err.Error(), true
			return m
		}
		m = m.adopt(forest)
	}
	m.spine.notice, m.spine.failed = notice, false
	return m
}

// suggestName proposes a branch name from the turn's own words. It is only a
// starting point: the field is editable, and a lane can always be renamed.
func suggestName(seg graph.Segment) string {
	words := strings.Fields(strings.ToLower(seg.Preview))
	var out []string
	for _, w := range words {
		w = strings.Trim(w, ".,:;!?\"'`()[]{}")
		if len(w) < 3 || stopWords[w] {
			continue
		}
		out = append(out, w)
		if len(out) == 3 {
			break
		}
	}
	if len(out) == 0 {
		return fmt.Sprintf("branch-t%d", seg.Seq)
	}
	return strings.Join(out, "-")
}

// stopWords are the words that make a name say nothing.
var stopWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "this": true, "that": true,
	"you": true, "can": true, "was": true, "are": true, "but": true, "not": true,
	"our": true, "its": true, "his": true, "her": true, "has": true, "had": true,
	"why": true, "how": true, "what": true, "when": true, "who": true, "does": true,
	"did": true, "will": true, "would": true, "should": true, "could": true,
	"from": true, "into": true, "your": true, "than": true, "then": true,
}

// promoteAgent turns the selected subagent into a conversation of its own.
func (m Model) promoteAgent() Model {
	s := m.spine
	row := s.current()
	if row.agent == nil {
		s.notice, s.failed = "p promotes a subagent — none is selected", true
		return m
	}
	if m.promote == nil {
		s.notice, s.failed = "promoting is unavailable for this source", true
		return m
	}
	id, err := m.promote(s.lane.ID, row.agent.ID)
	if err != nil {
		s.notice, s.failed = err.Error(), true
		return m
	}
	notice := fmt.Sprintf("promoted %s → %s", row.agent.Type, shortID(id))
	if m.refresh != nil {
		forest, refreshErr := m.refresh()
		if refreshErr != nil {
			s.notice, s.failed = notice+" · but the refresh failed: "+refreshErr.Error(), true
			return m
		}
		m = m.adopt(forest)
	}
	m.spine.notice, m.spine.failed = notice, false
	return m
}

// nextMarker finds the next place the conversation did something other than
// carry on, wrapping around: a branch kept inside the transcript, a branch that
// left for its own file, or an agent it spawned. Three things, one key —
// scrolling a 320-row spine to find any of them is not navigation.
func nextMarker(rows []spineRow, from, step int) int {
	for i := 1; i <= len(rows); i++ {
		at := wrap(from, i*step, len(rows))
		if marker(rows[at]) {
			return at
		}
	}
	return from
}

func marker(r spineRow) bool {
	return r.fork != nil || r.agent != nil || len(r.seg.Alternates) > 0
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

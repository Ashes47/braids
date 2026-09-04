package tui

import (
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"strings"

	"github.com/Ashes47/braids/internal/core/memory"
)

// The memory screen is read-only, and shaped so curation can be added to it
// without rework.
//
// What it is for: a memory is loaded into a session only if the index mentions
// it, so one can exist and do nothing — a failure that is invisible from inside
// a session and obvious from here. And most memories record the session that
// wrote them, which is a real edge back to the conversation where the decision
// was actually made.
//
// Where curation attaches: the cursor already identifies one memory, the notice
// line already reports what happened, and the state already reloads from disk
// after an action. A delete, rename or retype would add a key here and a
// function to Options, and would carry one hard obligation — change the index
// in the same breath as the file, because the index is the part that is read.
type memoryState struct {
	sets []memory.Set
	// set and cursor are which project and which memory within it. The two
	// lists are flattened for movement so j and k cross a project boundary
	// without a second key.
	rows []memoryRow
	// shown is rows after the filter, which is what the cursor indexes.
	shown  []memoryRow
	filter filterInput
	cursor int
	offset int
	err    error
	notice string
	failed bool
	// reading is the memory whose text is on screen, or nil while the list is.
	reading *memoryDoc
}

// memoryDoc is one memory being read: its text, wrapped to the frame, and how
// far down it the reader has scrolled.
type memoryDoc struct {
	memory memory.Memory
	text   string
	lines  []string
	offset int
	// width is what the text was wrapped to, so a resize re-wraps it rather
	// than leaving it ragged.
	width int
	err   error
}

// memoryRow is one line: either a project heading or a memory under it.
type memoryRow struct {
	project string
	// memory is nil on a heading row.
	memory *memory.Memory
	set    *memory.Set
}

const (
	memKindWidth  = 10
	memLinksWidth = 6
	memWhenWidth  = 8
	memFromWidth  = 10
)

func (m Model) openMemories() Model {
	if m.loadMemories == nil {
		return m.withNotice("this source keeps no memories", true)
	}
	sets, err := m.loadMemories()
	state := &memoryState{sets: sets, err: err}
	state.rows = memoryRows(state.sets)
	state.shown = state.rows
	m.memories = state
	m.returnTo = m.mode
	m.mode = memoryMode
	// Land on the first memory rather than a heading: a cursor on a label has
	// nothing to act on.
	m.memories.cursor = 0
	m = m.nextMemory(1, true)
	return m
}

// memoryRows flattens the sets into headings and memories.
func memoryRows(sets []memory.Set) []memoryRow {
	var rows []memoryRow
	for i := range sets {
		set := &sets[i]
		rows = append(rows, memoryRow{project: set.Project, set: set})
		for j := range set.Memories {
			rows = append(rows, memoryRow{project: set.Project, memory: &set.Memories[j], set: set})
		}
	}
	return rows
}

func (m Model) memoryKey(key string) Model {
	s := m.memories
	if s.reading != nil {
		return m.readingKey(key)
	}
	if s.filter.key(key) {
		m.applyMemoryFilter()
		return m
	}
	switch key {
	case "f":
		s.filter.active = true
	case "esc", "backspace", "h", "left":
		m.mode = m.returnTo
		m.memories = nil
		return m
	case "j", "down":
		return m.nextMemory(1, false)
	case "k", "up":
		return m.nextMemory(-1, false)
	case "g", "home":
		s.cursor = 0
		m = m.nextMemory(1, true)
	case "G", "end":
		s.cursor = len(s.shown) - 1
		m = m.nextMemory(-1, true)
	case "n":
		return m.nextFlagged(1)
	case "N":
		return m.nextFlagged(-1)
	case "enter", "l", "right":
		return m.readMemory()
	case "c":
		return m.openMemoryOrigin()
	}
	m.clampMemories()
	return m
}

// nextMemory moves to the next row that is a memory, skipping headings. When
// here is true the current row counts, so g and G can land rather than step.
func (m Model) nextMemory(step int, here bool) Model {
	s := m.memories
	if len(s.shown) == 0 {
		return m
	}
	at := s.cursor
	if !here {
		at = wrap(at, step, len(s.shown))
	}
	for range len(s.shown) {
		if s.shown[at].memory != nil {
			s.cursor = at
			m.clampMemories()
			return m
		}
		at = wrap(at, step, len(s.shown))
	}
	m.clampMemories()
	return m
}

// applyMemoryFilter narrows the list, keeping a project heading only while one
// of its memories is still showing: a heading over nothing is noise.
func (m *Model) applyMemoryFilter() {
	s := m.memories
	if !s.filter.on() {
		s.shown = s.rows
		m.clampMemories()
		m.landOnMemory()
		return
	}
	shown := make([]memoryRow, 0, len(s.rows))
	for i, r := range s.rows {
		if r.memory != nil {
			// Name, kind, description and project: what a person would
			// remember about a memory they are looking for.
			if s.filter.matches(r.memory.Name + " " + r.memory.Kind + " " +
				r.memory.Description + " " + r.project) {
				shown = append(shown, r)
			}
			continue
		}
		if memoriesMatch(s.rows[i+1:], s.filter) {
			shown = append(shown, r)
		}
	}
	s.shown = shown
	m.clampMemories()
	m.landOnMemory()
}

// landOnMemory keeps the cursor off a heading after the list changes under it.
func (m *Model) landOnMemory() {
	s := m.memories
	if s.cursor < len(s.shown) && s.shown[s.cursor].memory != nil {
		return
	}
	moved := m.nextMemory(1, true)
	m.memories = moved.memories
}

// memoriesMatch reports whether the rows before the next heading hold a match.
func memoriesMatch(rest []memoryRow, f filterInput) bool {
	for _, r := range rest {
		if r.memory == nil {
			return false
		}
		if f.matches(r.memory.Name + " " + r.memory.Kind + " " +
			r.memory.Description + " " + r.project) {
			return true
		}
	}
	return false
}

// openMemoryOrigin jumps to the conversation that wrote the memory under the
// cursor — the edge that makes this more than a file list.
func (m Model) openMemoryOrigin() Model {
	entry, ok := m.memoryCursor()
	switch {
	case !ok:
		return m
	case entry.Origin == "":
		m.memories.notice, m.memories.failed = "this memory did not record which conversation wrote it", true
		return m
	}
	for i, r := range m.all {
		if r.node.Lane.ID != entry.Origin {
			continue
		}
		m.mode = mapMode
		m.memories = nil
		m.cursor = i
		m.apply()
		m.clamp()
		return m.withNotice("the conversation that wrote "+entry.Name, false)
	}
	m.memories.notice = fmt.Sprintf("the conversation that wrote this is not indexed (%s)", shortID(entry.Origin))
	m.memories.failed = true
	return m
}

func (m Model) memoryCursor() (memory.Memory, bool) {
	s := m.memories
	if s == nil || s.cursor < 0 || s.cursor >= len(s.shown) || s.shown[s.cursor].memory == nil {
		return memory.Memory{}, false
	}
	return *s.shown[s.cursor].memory, true
}

func (m *Model) clampMemories() {
	s := m.memories
	if s == nil {
		return
	}
	s.cursor = min(max(s.cursor, 0), max(len(s.shown)-1, 0))
	h := m.bodyHeight()
	if s.cursor < s.offset {
		s.offset = s.cursor
	}
	if s.cursor >= s.offset+h {
		s.offset = s.cursor - h + 1
	}
	s.offset = max(s.offset, 0)
}

func (m Model) renderMemories() string {
	s := m.memories
	if s.reading != nil {
		return m.renderReading()
	}
	var out strings.Builder
	out.WriteString(m.memoryInfo())
	out.WriteString("\n\n")
	out.WriteString(m.panelTopTitled(fmt.Sprintf("Memories(%s)[%d]",
		orAll(s.filter.label()), m.memoryCount())))
	out.WriteString("\n")
	out.WriteString(m.framed(m.memoryColumns()))
	out.WriteString("\n")

	blank := repeat(" ", m.contentWidth())
	switch {
	case s.err != nil:
		out.WriteString(m.framed(padRight(" "+m.theme.Empty.Render(s.err.Error()), m.contentWidth())) + "\n")
		m.fill(&out, blank, m.bodyHeight()-1)
	case len(s.shown) == 0:
		out.WriteString(m.framed(padRight(" "+m.theme.Empty.Render(m.memoryEmpty()), m.contentWidth())) + "\n")
		m.fill(&out, blank, m.bodyHeight()-1)
	default:
		end := min(s.offset+m.bodyHeight(), len(s.shown))
		for i := s.offset; i < end; i++ {
			out.WriteString(m.framed(m.renderMemoryRow(s.shown[i], i == s.cursor)) + "\n")
		}
		m.fill(&out, blank, m.bodyHeight()-(end-s.offset))
	}
	out.WriteString(m.panelBottom())
	out.WriteString("\n")
	if prompt := m.filterPrompt(s.filter); prompt != "" {
		out.WriteString(" " + prompt)
	} else {
		out.WriteString(" " + m.memoryStatus())
	}
	return out.String()
}

// memoryStatus is the description of what the cursor is on, or the last notice.
func (m Model) memoryStatus() string {
	s := m.memories
	if s.notice != "" {
		return m.noticeStyle(s.failed).Render(truncate(s.notice, m.width-2))
	}
	entry, ok := m.memoryCursor()
	if !ok {
		return ""
	}
	return m.theme.Label.Render(truncate(entry.Description, m.width-2))
}

// memoryEmpty says why the list is empty: nothing remembered, or nothing
// matching.
func (m Model) memoryEmpty() string {
	if m.memories.filter.on() {
		return fmt.Sprintf("nothing matches %q", m.memories.filter.text)
	}
	return "nothing is remembered yet"
}

func (m Model) memoryCount() int {
	n := 0
	for _, r := range m.memories.shown {
		if r.memory != nil {
			n++
		}
	}
	return n
}

func (m Model) memoryFacts() []fact {
	s := m.memories
	var bytes int64
	unlisted, orphaned, dangling := 0, 0, 0
	for i := range s.sets {
		bytes += s.sets[i].Bytes()
		unlisted += len(s.sets[i].Unlisted())
		orphaned += len(s.sets[i].Orphaned)
		dangling += len(s.sets[i].Dangling())
	}
	// Three separate numbers because they mean three different things: a
	// memory the index omits is loaded by nothing and is broken; an index row
	// with no file is a stale pointer; a link to a name that does not exist
	// yet is a legitimate note to self.
	return []fact{
		{"Remembered", fmt.Sprintf("%d in %d projects", m.memoryCount(), len(s.sets))},
		{"Holding", humanBytes(bytes)},
		{"Never loaded", fmt.Sprintf("%d", unlisted)},
		{"Index stale", fmt.Sprintf("%d", orphaned)},
		{"Loose links", fmt.Sprintf("%d", dangling)},
	}
}

func memoryHints() []hint {
	return []hint{
		{"j/k", "down / up"}, {"↵", "read it"},
		{"c", "the conversation"}, {"n / N", "next / prev flagged"},
		{"f", "filter"}, {"esc", "back"},
	}
}

func readingHints() []hint {
	return []hint{
		{"j/k", "scroll"}, {"c", "the conversation"},
		{"esc", "back to the list"}, {"q", "quit"},
	}
}

func (m Model) memoryInfo() string {
	if m.memories.reading != nil {
		return m.factsBlock(m.readingFacts(), readingHints(), nil)
	}
	return m.factsBlock(m.memoryFacts(), memoryHints(), m.memoryGlyphs())
}

func (m Model) memoryGlyphs() []glyph {
	g := m.theme.Glyphs
	return []glyph{
		{g.Failed, m.theme.Urgent, "not in the index — never loaded"},
		{g.Agent, m.theme.Accent, "points at a memory that is missing"},
	}
}

func (m Model) memoryColumns() string {
	return m.theme.Column.Render(" " + padRight("MEMORY", m.memoryNameWidth()) + " " +
		padRight("KIND", memKindWidth) + " " + padLeft("LINKS", memLinksWidth) + " " +
		padLeft("CHANGED", memWhenWidth) + " " + padLeft("FROM", memFromWidth))
}

func (m Model) memoryNameWidth() int {
	return max(m.contentWidth()-5-memKindWidth-memLinksWidth-memWhenWidth-memFromWidth, 10)
}

func (m Model) renderMemoryRow(r memoryRow, selected bool) string {
	if r.memory == nil {
		// A project heading: the sets are shown together, so the boundary has
		// to be visible without costing a column.
		label := fmt.Sprintf("%s · %d", r.project, len(r.set.Memories))
		return " " + m.theme.Title.Render(padRight(truncate(label, m.contentWidth()-2), m.contentWidth()-2)) + " "
	}
	entry := *r.memory
	marks, markStyles := m.memoryMarks(r, entry)
	label := "  " + marks
	if marks != "" {
		label += " "
	}
	label += entry.Name
	nameCell := padRight(truncate(label, m.memoryNameWidth()), m.memoryNameWidth())
	kind := padRight(truncate(entry.Kind, memKindWidth), memKindWidth)
	links := padLeft(fmt.Sprintf("%d", len(entry.Links)), memLinksWidth)
	when := padLeft(entry.Modified.Format("01-02"), memWhenWidth)
	from := padLeft(orDash(shortID(entry.Origin)), memFromWidth)

	plain := " " + nameCell + " " + kind + " " + links + " " + when + " " + from
	if selected {
		// One background across the row: styling inside it would tear the fill.
		return m.theme.Selected.Width(m.contentWidth()).Render(plain)
	}
	style := m.theme.Value
	if !entry.Listed {
		style = m.theme.Urgent
	}
	// The marks carry the same colour here as in the legend. A legend that
	// teaches a colour the rows do not use teaches nothing.
	return " " + markStyles + style.Render(strings.TrimPrefix(nameCell, "  "+marks)) + " " +
		m.theme.Faint.Render(kind) + " " + m.theme.Faint.Render(links) + " " +
		m.theme.Faint.Render(when) + " " + m.theme.Faint.Render(from)
}

// memoryMarks are the flags on a memory: the plain characters, and the same
// characters wearing the colours the legend gives them.
func (m Model) memoryMarks(r memoryRow, entry memory.Memory) (plain, styled string) {
	styled = "  "
	if !entry.Listed {
		plain += m.theme.Glyphs.Failed
		styled += m.theme.Urgent.Render(m.theme.Glyphs.Failed)
	}
	if len(danglingFrom(r.set, entry.Name)) > 0 {
		plain += m.theme.Glyphs.Agent
		styled += m.theme.Accent.Render(m.theme.Glyphs.Agent)
	}
	return plain, styled
}

// flagged reports whether a memory has anything wrong with it, which is what
// n and N step between.
func (m Model) flagged(r memoryRow) bool {
	if r.memory == nil {
		return false
	}
	return !r.memory.Listed || len(danglingFrom(r.set, r.memory.Name)) > 0
}

// nextFlagged moves to the next memory with a flag on it. Nothing flagged is
// said rather than silently doing nothing.
func (m Model) nextFlagged(step int) Model {
	s := m.memories
	if len(s.shown) == 0 {
		return m
	}
	at := s.cursor
	for range len(s.shown) {
		at = wrap(at, step, len(s.shown))
		if m.flagged(s.shown[at]) {
			s.cursor = at
			s.notice = ""
			m.clampMemories()
			return m
		}
	}
	s.notice, s.failed = "nothing here is unlisted or pointing at a missing memory", false
	return m
}

// danglingFrom is the loose links one memory has.
func danglingFrom(set *memory.Set, name string) []memory.Link {
	var out []memory.Link
	for _, l := range set.Dangling() {
		if l.From == name {
			out = append(out, l)
		}
	}
	return out
}

// readMemory opens the memory under the cursor for reading. The list says what
// braids knows *about* a memory; this is the memory itself, which is the thing
// you actually came to check.
func (m Model) readMemory() Model {
	entry, ok := m.memoryCursor()
	if !ok {
		return m
	}
	body, err := memory.Body(entry.Path)
	doc := &memoryDoc{memory: entry, text: body, err: err}
	doc.rewrap(m.contentWidth() - 2)
	m.memories.reading = doc
	m.memories.notice, m.memories.failed = "", false
	return m
}

func (m Model) readingKey(key string) Model {
	doc := m.memories.reading
	switch key {
	case "esc", "backspace", "h", "left":
		m.memories.reading = nil
	case "j", "down":
		doc.offset++
	case "k", "up":
		doc.offset--
	case "g", "home":
		doc.offset = 0
	case "G", "end":
		doc.offset = len(doc.lines)
	case "ctrl+d", "pgdown", " ", "space":
		doc.offset += m.bodyHeight()
	case "ctrl+u", "pgup":
		doc.offset -= m.bodyHeight()
	case "c":
		m.memories.reading = nil
		return m.openMemoryOrigin()
	}
	m.clampReading()
	return m
}

func (m *Model) clampReading() {
	doc := m.memories.reading
	if doc == nil {
		return
	}
	// Stop with the last line on screen rather than scrolling past the end
	// into blank space.
	doc.offset = min(max(doc.offset, 0), max(len(doc.lines)-m.bodyHeight(), 0))
}

// rewrap lays the text out for a frame of this width. Wrapping on words, not
// characters: a memory is prose.
func (doc *memoryDoc) rewrap(width int) {
	if width < 20 {
		width = 20
	}
	if doc.width == width && doc.lines != nil {
		return
	}
	doc.width = width
	doc.lines = nil
	for _, para := range strings.Split(ansi.Wordwrap(doc.text, width, " -"), "\n") {
		doc.lines = append(doc.lines, para)
	}
}

func (m Model) readingFacts() []fact {
	doc := m.memories.reading
	return []fact{
		{"Memory", doc.memory.Name},
		{"Kind", orDash(doc.memory.Kind)},
		{"Changed", doc.memory.Modified.Format("2006-01-02")},
		{"Written by", orDash(shortID(doc.memory.Origin))},
		{"Links", strings.Join(doc.memory.Links, ", ")},
	}
}

func (m Model) renderReading() string {
	doc := m.memories.reading
	doc.rewrap(m.contentWidth() - 2)
	m.clampReading()

	var out strings.Builder
	out.WriteString(m.memoryInfo())
	out.WriteString("\n\n")
	title := doc.memory.Name
	if !doc.memory.Listed {
		title += " · not in the index"
	}
	out.WriteString(m.panelTopTitled(truncate(title, m.contentWidth()-6)))
	out.WriteString("\n")

	blank := repeat(" ", m.contentWidth())
	switch {
	case doc.err != nil:
		out.WriteString(m.framed(padRight(" "+m.theme.Empty.Render(doc.err.Error()), m.contentWidth())) + "\n")
		m.fill(&out, blank, m.bodyHeight()-1)
	default:
		end := min(doc.offset+m.bodyHeight(), len(doc.lines))
		for i := doc.offset; i < end; i++ {
			out.WriteString(m.framed(padRight(" "+m.theme.Value.Render(doc.lines[i]), m.contentWidth())) + "\n")
		}
		m.fill(&out, blank, m.bodyHeight()-(end-doc.offset))
	}
	out.WriteString(m.panelBottom())
	out.WriteString("\n")
	out.WriteString(" " + m.theme.Label.Render(truncate(doc.memory.Description, m.width-2)))
	return out.String()
}

package tui

import (
	"fmt"
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
	// naming is the rename field, opened with r on a memory.
	naming filterInput
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
	if s.naming.active {
		return m.renameMemoryKey(key)
	}
	if s.filter.key(key) {
		m.applyMemoryFilter()
		return m
	}
	switch key {
	case "f":
		s.filter.active = true
	case "d":
		return m.removeMemory()
	case "i":
		return m.repairMemoryIndex()
	case "r":
		return m.startMemoryRename()
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
		return m.nextMarked(1)
	case "N":
		return m.nextMarked(-1)
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
	if s.naming.active {
		return m.theme.Accent.Render("rename to: ") + m.theme.Value.Render(s.naming.text) +
			m.theme.Accent.Render("▏") + m.theme.Label.Render("  enter renames · esc cancels")
	}
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
		{"c", "the conversation"}, {"n / N", "next / prev marked"},
		{"r", "rename"}, {"i", "repair the index"},
		{"d", "delete"}, {"f", "filter"},
		{"esc", "back"},
	}
}

func readingHints() []hint {
	return []hint{
		{"j/k", "scroll"}, {"c", "the conversation"},
		{"esc", "back to the list"}, {"q", "quit"},
	}
}

// memoryInfo draws the header for whichever of the two screens is showing.
// What it draws comes from headerContent, which is also what sizes it — a
// header measured from one screen and drawn from another overflows.
func (m Model) memoryInfo() string {
	facts, keys, glyphs := m.headerContent()
	return m.factsBlock(facts, keys, glyphs)
}

func (m Model) memoryGlyphs() []glyph {
	g := m.theme.Glyphs
	return []glyph{
		{g.Failed, m.theme.Urgent, "not in the index — never loaded"},
		{g.Agent, m.theme.Accent, "links to one not written yet"},
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

// marked reports whether a memory carries either mark: not in the index, which
// is broken, or waiting on a memory not written yet, which is a note. n and N
// step between both, because both are worth walking to.
func (m Model) marked(r memoryRow) bool {
	if r.memory == nil {
		return false
	}
	return !r.memory.Listed || len(danglingFrom(r.set, r.memory.Name)) > 0
}

// nextMarked moves to the next memory carrying a mark. Nothing marked is said
// rather than silently doing nothing.
func (m Model) nextMarked(step int) Model {
	s := m.memories
	if len(s.shown) == 0 {
		return m
	}
	at := s.cursor
	for range len(s.shown) {
		at = wrap(at, step, len(s.shown))
		if m.marked(s.shown[at]) {
			s.cursor = at
			s.notice = ""
			m.clampMemories()
			return m
		}
	}
	s.notice, s.failed = "nothing here is unlisted, and no link is waiting on a memory", false
	return m
}

// looseNote names the links a memory is waiting on, and says they are not a
// fault.
//
// A link to a memory that does not exist yet is how the convention marks
// something worth writing later, so braids reports it and repairs nothing: the
// only ways to "fix" one are to delete somebody's note, invent the memory it
// points at, or guess which existing memory was meant.
func looseNote(r memoryRow) string {
	if r.memory == nil {
		return ""
	}
	loose := danglingFrom(r.set, r.memory.Name)
	if len(loose) == 0 {
		return ""
	}
	targets := make([]string, 0, len(loose))
	for _, l := range loose {
		targets = append(targets, "[["+l.To+"]]")
	}
	return fmt.Sprintf(" · %s is waiting on %s, which is a note rather than a fault",
		r.memory.Name, strings.Join(targets, " and "))
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
	m.rewrap(doc, m.contentWidth()-2)
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

// rewrap lays the text out for a frame of this width, as markdown: the reader
// sees emphasis rather than the asterisks that mean it.
func (m Model) rewrap(doc *memoryDoc, width int) {
	if width < 20 {
		width = 20
	}
	if doc.width == width && doc.lines != nil {
		return
	}
	doc.width = width
	doc.lines = m.prose(doc.text, width)
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
	m.rewrap(doc, m.contentWidth()-2)
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
			// The lines arrive already styled as markdown, so they are placed
			// rather than styled again.
			out.WriteString(m.framed(padRight(" "+doc.lines[i], m.contentWidth())) + "\n")
		}
		m.fill(&out, blank, m.bodyHeight()-(end-doc.offset))
	}
	out.WriteString(m.panelBottom())
	out.WriteString("\n")
	out.WriteString(" " + m.theme.Label.Render(truncate(doc.memory.Description, m.width-2)))
	return out.String()
}

// Curation. Every operation here changes the index in the same breath as the
// file, because the index is what a session loads: a memory deleted without its
// row leaves a pointer to nothing, and one renamed without its row becomes
// invisible while still sitting on disk.

// liveIn names a conversation in a project that is being written right now.
//
// braids editing a file a session may also be writing breaks one writer per
// file, and the thing at stake is something the person asked to be remembered.
// So an edit is refused while anything in that project is live, which is
// stricter than it needs to be and cheaper than being wrong.
func (m Model) liveIn(project string) (string, bool) {
	for _, r := range m.all {
		if r.node.Lane.Project != project {
			continue
		}
		switch stateOf(r.node.Lane, m.liveFor(r.node.Lane.ID), m.now()) {
		case stateWorking, stateThinking, stateNeedsYou:
			name := r.node.Lane.Title
			if name == "" {
				name = shortID(r.node.Lane.ID)
			}
			return name, true
		}
	}
	return "", false
}

// guardMemoryEdit refuses an edit while the project is busy, saying which
// conversation is holding it up.
func (m Model) guardMemoryEdit(project string) (Model, bool) {
	who, live := m.liveIn(project)
	if !live {
		return m, true
	}
	m.memories.notice = fmt.Sprintf("%q is running — braids will not edit memories under it", truncate(who, 40))
	m.memories.failed = true
	return m, false
}

func (m Model) removeMemory() Model {
	entry, ok := m.memoryCursor()
	if !ok || m.removeMemoryFn == nil {
		return m
	}
	row := m.memories.shown[m.memories.cursor]
	if guarded, allowed := m.guardMemoryEdit(row.project); !allowed {
		return guarded
	}
	if err := m.removeMemoryFn(row.set.Dir, entry.Name); err != nil {
		m.memories.notice, m.memories.failed = err.Error(), true
		return m
	}
	m = m.reloadMemories()
	m.memories.notice = fmt.Sprintf("%s is in the bin, and out of the index · u to recover", entry.Name)
	m.memories.failed = false
	return m
}

func (m Model) repairMemoryIndex() Model {
	if m.repairMemoriesFn == nil || len(m.memories.shown) == 0 {
		return m
	}
	row := m.memories.shown[min(m.memories.cursor, len(m.memories.shown)-1)]
	if guarded, allowed := m.guardMemoryEdit(row.project); !allowed {
		return guarded
	}
	added, dropped, err := m.repairMemoriesFn(row.set.Dir)
	if err != nil {
		m.memories.notice, m.memories.failed = err.Error(), true
		return m
	}
	m = m.reloadMemories()
	switch {
	case added == 0 && dropped == 0:
		// Saying only "nothing to do" while the row under the cursor wears a
		// mark reads as a refusal to explain. The mark is about links, which
		// are not the index's business.
		m.memories.notice = fmt.Sprintf("%s: the index already agrees with the files%s",
			row.project, looseNote(row))
	default:
		m.memories.notice = fmt.Sprintf("%s: listed %d that nothing loaded, dropped %d pointing at nothing",
			row.project, added, dropped)
	}
	m.memories.failed = false
	return m
}

func (m Model) startMemoryRename() Model {
	entry, ok := m.memoryCursor()
	if !ok || m.renameMemoryFn == nil {
		return m
	}
	m.memories.naming = filterInput{active: true, text: entry.Name}
	return m
}

func (m Model) renameMemoryKey(key string) Model {
	s := m.memories
	switch key {
	case "esc":
		s.naming = filterInput{}
		return m
	case "enter":
		return m.finishMemoryRename()
	}
	s.naming.edit(key)
	return m
}

func (m Model) finishMemoryRename() Model {
	s := m.memories
	entry, ok := m.memoryCursor()
	to := strings.TrimSpace(s.naming.text)
	s.naming = filterInput{}
	if !ok || to == "" || to == entry.Name {
		return m
	}
	row := s.shown[s.cursor]
	if guarded, allowed := m.guardMemoryEdit(row.project); !allowed {
		return guarded
	}
	relinked, err := m.renameMemoryFn(row.set.Dir, entry.Name, to)
	if err != nil {
		s.notice, s.failed = err.Error(), true
		return m
	}
	m = m.reloadMemories()
	m.memories.notice = fmt.Sprintf("%s is now %s%s", entry.Name, to, relinkedNote(relinked))
	m.memories.failed = false
	return m
}

// relinkedNote says how many references were followed, because a rename that
// silently rewrote a dozen other memories should say so.
func relinkedNote(n int) string {
	switch n {
	case 0:
		return ""
	case 1:
		return " · one link followed"
	default:
		return fmt.Sprintf(" · %d links followed", n)
	}
}

// reloadMemories reads the set again after changing it, keeping the cursor
// where it can.
func (m Model) reloadMemories() Model {
	if m.loadMemories == nil {
		return m
	}
	at, filter := m.memories.cursor, m.memories.filter
	sets, err := m.loadMemories()
	state := &memoryState{sets: sets, err: err, filter: filter, cursor: at}
	state.rows = memoryRows(state.sets)
	state.shown = state.rows
	m.memories = state
	m.applyMemoryFilter()
	return m
}

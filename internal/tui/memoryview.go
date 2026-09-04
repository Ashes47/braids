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
	rows   []memoryRow
	cursor int
	offset int
	err    error
	notice string
	failed bool
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
	switch key {
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
		s.cursor = len(s.rows) - 1
		m = m.nextMemory(-1, true)
	case "enter":
		return m.openMemoryOrigin()
	}
	m.clampMemories()
	return m
}

// nextMemory moves to the next row that is a memory, skipping headings. When
// here is true the current row counts, so g and G can land rather than step.
func (m Model) nextMemory(step int, here bool) Model {
	s := m.memories
	if len(s.rows) == 0 {
		return m
	}
	at := s.cursor
	if !here {
		at = wrap(at, step, len(s.rows))
	}
	for range len(s.rows) {
		if s.rows[at].memory != nil {
			s.cursor = at
			m.clampMemories()
			return m
		}
		at = wrap(at, step, len(s.rows))
	}
	m.clampMemories()
	return m
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
	if s == nil || s.cursor < 0 || s.cursor >= len(s.rows) || s.rows[s.cursor].memory == nil {
		return memory.Memory{}, false
	}
	return *s.rows[s.cursor].memory, true
}

func (m *Model) clampMemories() {
	s := m.memories
	if s == nil {
		return
	}
	s.cursor = min(max(s.cursor, 0), max(len(s.rows)-1, 0))
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
	var out strings.Builder
	out.WriteString(m.memoryInfo())
	out.WriteString("\n\n")
	out.WriteString(m.panelTopTitled(fmt.Sprintf("Memories[%d]", m.memoryCount())))
	out.WriteString("\n")
	out.WriteString(m.framed(m.memoryColumns()))
	out.WriteString("\n")

	blank := repeat(" ", m.contentWidth())
	switch {
	case s.err != nil:
		out.WriteString(m.framed(padRight(" "+m.theme.Empty.Render(s.err.Error()), m.contentWidth())) + "\n")
		m.fill(&out, blank, m.bodyHeight()-1)
	case len(s.rows) == 0:
		out.WriteString(m.framed(padRight(" "+m.theme.Empty.Render("nothing is remembered yet"), m.contentWidth())) + "\n")
		m.fill(&out, blank, m.bodyHeight()-1)
	default:
		end := min(s.offset+m.bodyHeight(), len(s.rows))
		for i := s.offset; i < end; i++ {
			out.WriteString(m.framed(m.renderMemoryRow(s.rows[i], i == s.cursor)) + "\n")
		}
		m.fill(&out, blank, m.bodyHeight()-(end-s.offset))
	}
	out.WriteString(m.panelBottom())
	out.WriteString("\n")
	out.WriteString(" " + m.memoryStatus())
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

func (m Model) memoryCount() int {
	n := 0
	for _, r := range m.memories.rows {
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
		{"j/k", "down / up"}, {"↵", "open the conversation"},
		{"esc", "back"}, {"q", "quit"},
	}
}

func (m Model) memoryInfo() string {
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
	marks := ""
	if !entry.Listed {
		marks += m.theme.Glyphs.Failed
	}
	if len(danglingFrom(r.set, entry.Name)) > 0 {
		marks += m.theme.Glyphs.Agent
	}
	name := entry.Name
	if marks != "" {
		name = marks + " " + name
	}
	nameCell := padRight(truncate("  "+name, m.memoryNameWidth()), m.memoryNameWidth())
	kind := padRight(truncate(entry.Kind, memKindWidth), memKindWidth)
	links := padLeft(fmt.Sprintf("%d", len(entry.Links)), memLinksWidth)
	when := padLeft(entry.Modified.Format("01-02"), memWhenWidth)
	from := padLeft(orDash(shortID(entry.Origin)), memFromWidth)

	plain := " " + nameCell + " " + kind + " " + links + " " + when + " " + from
	if selected {
		return m.theme.Selected.Width(m.contentWidth()).Render(plain)
	}
	style := m.theme.Value
	if !entry.Listed {
		style = m.theme.Urgent
	}
	return " " + style.Render(nameCell) + " " + m.theme.Faint.Render(kind) + " " +
		m.theme.Faint.Render(links) + " " + m.theme.Faint.Render(when) + " " +
		m.theme.Faint.Render(from)
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

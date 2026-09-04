package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Ashes47/braids/internal/core/artifacts"
)

// The work-products screen is a size browser, not a file list. A session's
// scratch outweighs its transcript by an order of magnitude and arrives as
// thousands of files in a tree a dozen levels deep, so listing them flat would
// be accurate and useless. One level at a time, heaviest first, directories
// weighed by everything under them: the same shape du and ncdu settled on,
// because the question is always which single branch is holding the room.
type workState struct {
	lane string
	// root is the top of the conversation's work products; dir is where the
	// cursor currently is, always inside root.
	root, dir string
	entries   []artifacts.Entry
	cursor    int
	offset    int
	err       error
	notice    string
	failed    bool
}

// WorkLevel is one level of a conversation's work products.
type WorkLevel struct {
	Root, Dir string
	Entries   []artifacts.Entry
}

const (
	workSizeWidth  = 9
	workFilesWidth = 8
)

func (m Model) openWork() Model {
	if m.loadWork == nil {
		return m.withNotice("work products are unavailable", true)
	}
	lane, ok := m.selected()
	if !ok {
		return m
	}
	level, err := m.loadWork(lane.node.Lane.ID, "")
	if err != nil {
		return m.withNotice(err.Error(), true)
	}
	m.work = &workState{lane: lane.node.Lane.ID, root: level.Root, dir: level.Dir, entries: level.Entries}
	m.returnTo = m.mode
	m.mode = workMode
	return m
}

func (m Model) workKey(key string) Model {
	w := m.work
	switch key {
	case "esc", "backspace", "h", "left":
		// Up a level, and out only from the top: the obvious key for "back"
		// should retrace the way in rather than abandon it.
		if w.dir == w.root {
			m.mode = m.returnTo
			m.work = nil
			return m
		}
		return m.enterWork(filepath.Dir(w.dir))
	case "j", "down":
		w.cursor = wrap(w.cursor, 1, len(w.entries))
	case "k", "up":
		w.cursor = wrap(w.cursor, -1, len(w.entries))
	case "g", "home":
		w.cursor = 0
	case "G", "end":
		w.cursor = len(w.entries) - 1
	case "enter", "l", "right":
		if e, ok := m.workCursor(); ok && e.Dir {
			return m.enterWork(e.Path)
		}
	case "d":
		return m.discardWorkEntry()
	}
	m.clampWork()
	return m
}

// enterWork moves to a directory, keeping the cursor at the top: a new level is
// a new question, and the answer to it is the heaviest row.
func (m Model) enterWork(dir string) Model {
	w := m.work
	rel, err := filepath.Rel(w.root, dir)
	if err != nil || strings.HasPrefix(rel, "..") {
		return m
	}
	if rel == "." {
		rel = ""
	}
	level, err := m.loadWork(w.lane, rel)
	if err != nil {
		w.notice, w.failed = err.Error(), true
		return m
	}
	w.dir, w.entries, w.cursor, w.offset = level.Dir, level.Entries, 0, 0
	w.notice, w.failed = "", false
	m.clampWork()
	return m
}

func (m Model) workCursor() (artifacts.Entry, bool) {
	w := m.work
	if w == nil || w.cursor < 0 || w.cursor >= len(w.entries) {
		return artifacts.Entry{}, false
	}
	return w.entries[w.cursor], true
}

// discardWorkEntry moves one file or directory to the bin. Recoverable, like
// every other deletion in braids: the point of picking one file out of a tree
// is to reclaim room, and being wrong about which file should cost nothing.
func (m Model) discardWorkEntry() Model {
	w := m.work
	entry, ok := m.workCursor()
	switch {
	case !ok || m.discardPaths == nil:
		return m
	case entry.Reserved:
		w.notice, w.failed = "that is the harness's own record of the job, not work — braids leaves it alone", true
		return m
	}
	bytes, err := m.discardPaths("work products: "+entry.Name, []string{entry.Path})
	if err != nil {
		w.notice, w.failed = err.Error(), true
		return m
	}
	was := w.cursor
	// Re-read the conversation list as well as this level: the map carries a
	// work-products column, and a stale number there is a lie about a disk.
	m = m.catchUp()
	m = m.enterWork(w.dir)
	m.work.cursor = max(min(was, len(m.work.entries)-1), 0)
	// Say plainly that the room is not back yet: the bin still holds it, and a
	// person deleting a 231 MB dump is watching a disk, not a list.
	m.work.notice = fmt.Sprintf("%s moved to the bin — still holding %s until it expires",
		entry.Name, humanBytes(bytes))
	m.work.failed = false
	m.clampWork()
	return m
}

func (m *Model) clampWork() {
	w := m.work
	if w == nil {
		return
	}
	w.cursor = min(max(w.cursor, 0), max(len(w.entries)-1, 0))
	h := m.bodyHeight()
	if w.cursor < w.offset {
		w.offset = w.cursor
	}
	if w.cursor >= w.offset+h {
		w.offset = w.cursor - h + 1
	}
	w.offset = max(w.offset, 0)
}

func (m Model) renderWork() string {
	w := m.work
	var out strings.Builder
	out.WriteString(m.workInfo())
	out.WriteString("\n\n")
	out.WriteString(m.panelTopTitled(fmt.Sprintf("Work products(%s)[%d]", m.workWhere(), len(w.entries))))
	out.WriteString("\n")
	out.WriteString(m.framed(m.workColumns()))
	out.WriteString("\n")

	blank := repeat(" ", m.contentWidth())
	switch {
	case w.err != nil:
		out.WriteString(m.framed(padRight(" "+m.theme.Empty.Render(w.err.Error()), m.contentWidth())) + "\n")
		m.fill(&out, blank, m.bodyHeight()-1)
	case len(w.entries) == 0:
		out.WriteString(m.framed(padRight(" "+m.theme.Empty.Render("nothing here"), m.contentWidth())) + "\n")
		m.fill(&out, blank, m.bodyHeight()-1)
	default:
		end := min(w.offset+m.bodyHeight(), len(w.entries))
		for i := w.offset; i < end; i++ {
			out.WriteString(m.framed(m.renderWorkRow(w.entries[i], i == w.cursor)) + "\n")
		}
		m.fill(&out, blank, m.bodyHeight()-(end-w.offset))
	}
	out.WriteString(m.panelBottom())
	out.WriteString("\n")
	if w.notice != "" {
		out.WriteString(" " + m.noticeStyle(w.failed).Render(truncate(w.notice, m.width-2)))
	}
	return out.String()
}

// workWhere is where the cursor is, relative to the top of the work products.
func (m Model) workWhere() string {
	rel, err := filepath.Rel(m.work.root, m.work.dir)
	if err != nil || rel == "." {
		return "top"
	}
	return rel
}

func (m Model) workFacts() []fact {
	var bytes int64
	var files int
	for _, e := range m.work.entries {
		bytes += e.Bytes
		files += e.Files
	}
	return []fact{
		{"Conversation", shortID(m.work.lane)},
		{"Here", m.workWhere()},
		{"Holding", humanBytes(bytes)},
		{"Files", fmt.Sprintf("%d", files)},
	}
}

func workHints() []hint {
	return []hint{
		{"j/k", "down / up"}, {"↵", "open directory"},
		{"d", "delete to bin"}, {"esc", "up a level"},
		{"q", "quit"},
	}
}

func (m Model) workInfo() string { return m.factsBlock(m.workFacts(), workHints(), nil) }

func (m Model) workColumns() string {
	return m.theme.Column.Render(" " + padLeft("SIZE", workSizeWidth) + " " +
		padLeft("FILES", workFilesWidth) + "  " + padRight("NAME", m.workNameWidth()))
}

func (m Model) workNameWidth() int {
	return max(m.contentWidth()-4-workSizeWidth-workFilesWidth, 8)
}

func (m Model) renderWorkRow(entry artifacts.Entry, selected bool) string {
	label := entry.Name
	if entry.Dir {
		label += "/"
	}
	if entry.Reserved {
		label += "  · the harness's own record"
	}
	name := padRight(truncate(label, m.workNameWidth()), m.workNameWidth())
	size := padLeft(humanBytes(entry.Bytes), workSizeWidth)
	files := padLeft(fmt.Sprintf("%d", entry.Files), workFilesWidth)

	plain := " " + size + " " + files + "  " + name
	if selected {
		return m.theme.Selected.Width(m.contentWidth()).Render(plain)
	}
	style := m.theme.Value
	if entry.Reserved {
		// Dull, not hidden: the sizes have to add up to the level above, so a
		// row braids will not touch still has to be shown.
		style = m.theme.Dim
	}
	return " " + m.theme.Faint.Render(size) + " " + m.theme.Faint.Render(files) + "  " + style.Render(name)
}

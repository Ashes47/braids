package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

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
	// shown is entries after the filter, which is what the cursor indexes.
	shown  []artifacts.Entry
	filter filterInput
	cursor int
	offset int
	err    error
	notice string
	failed bool
	// reading is the work product whose head is on screen, or nil while the
	// listing is.
	reading *workDoc
}

// workDoc is one work product being looked at: as much of its head as braids
// read, laid out for the frame.
type workDoc struct {
	entry  artifacts.Entry
	peek   artifacts.Peek
	lines  []string
	offset int
	width  int
	err    error
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
	m.work = &workState{lane: lane.node.Lane.ID, root: level.Root, dir: level.Dir,
		entries: level.Entries, shown: level.Entries}
	m.returnTo = m.mode
	m.mode = workMode
	return m
}

// workKey handles a keypress on either work screen. It returns a command
// because copying a path is one: the clipboard is reached through the terminal,
// not through a function call.
func (m Model) workKey(key string) (Model, tea.Cmd) {
	w := m.work
	if w.reading != nil {
		return m.readingWorkKey(key)
	}
	if w.filter.key(key) {
		m.applyWorkFilter()
		return m, nil
	}
	switch key {
	case "f":
		w.filter.active = true
	case "esc", "backspace", "h", "left":
		// Up a level, and out only from the top: the obvious key for "back"
		// should retrace the way in rather than abandon it.
		if w.dir == w.root {
			m.mode = m.returnTo
			m.work = nil
			return m, nil
		}
		return m.enterWork(filepath.Dir(w.dir)), nil
	case "j", "down":
		w.cursor = wrap(w.cursor, 1, len(w.shown))
	case "k", "up":
		w.cursor = wrap(w.cursor, -1, len(w.shown))
	case "g", "home":
		w.cursor = 0
	case "G", "end":
		w.cursor = len(w.shown) - 1
	case "enter", "l", "right":
		e, ok := m.workCursor()
		switch {
		case !ok:
		case e.Dir:
			return m.enterWork(e.Path), nil
		default:
			return m.openWorkFile(e), nil
		}
	case "d":
		return m.discardWorkEntry(), nil
	}
	m.clampWork()
	return m, nil
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
	// The filter follows you down: hunting one name through a tree is the
	// reason to have typed it.
	m.applyWorkFilter()
	return m
}

func (m Model) workCursor() (artifacts.Entry, bool) {
	w := m.work
	if w == nil || w.cursor < 0 || w.cursor >= len(w.shown) {
		return artifacts.Entry{}, false
	}
	return w.shown[w.cursor], true
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
		w.notice, w.failed = "that is the harness's own record of the job, not work: braids leaves it alone", true
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
	m.work.cursor = max(min(was, len(m.work.shown)-1), 0)
	// Say plainly that the room is not back yet: the bin still holds it, and a
	// person deleting a 231 MB dump is watching a disk, not a list.
	m.work.notice = fmt.Sprintf("%s moved to the bin, still holding %s until it expires",
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
	w.cursor = min(max(w.cursor, 0), max(len(w.shown)-1, 0))
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
	if w.reading != nil {
		return m.renderWorkFile()
	}
	var out strings.Builder
	out.WriteString(m.workInfo())
	out.WriteString("\n\n")
	out.WriteString(m.panelTopTitled(fmt.Sprintf("Work products(%s%s)[%d]",
		m.workWhere(), w.filter.label(), len(w.shown))))
	out.WriteString("\n")
	out.WriteString(m.framed(m.workColumns()))
	out.WriteString("\n")

	blank := repeat(" ", m.contentWidth())
	switch {
	case w.err != nil:
		out.WriteString(m.framed(padRight(" "+m.theme.Empty.Render(w.err.Error()), m.contentWidth())) + "\n")
		m.fill(&out, blank, m.bodyHeight()-1)
	case len(w.shown) == 0:
		out.WriteString(m.framed(padRight(" "+m.theme.Empty.Render(m.workEmpty()), m.contentWidth())) + "\n")
		m.fill(&out, blank, m.bodyHeight()-1)
	default:
		end := min(w.offset+m.bodyHeight(), len(w.shown))
		for i := w.offset; i < end; i++ {
			out.WriteString(m.framed(m.renderWorkRow(w.shown[i], i == w.cursor)) + "\n")
		}
		m.fill(&out, blank, m.bodyHeight()-(end-w.offset))
	}
	out.WriteString(m.panelBottom())
	out.WriteString("\n")
	if prompt := m.filterPrompt(w.filter); prompt != "" {
		out.WriteString(" " + prompt)
	} else if w.notice != "" {
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
	for _, e := range m.work.shown {
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

// applyWorkFilter narrows this level by name.
func (m *Model) applyWorkFilter() {
	w := m.work
	if !w.filter.on() {
		w.shown = w.entries
		m.clampWork()
		return
	}
	shown := make([]artifacts.Entry, 0, len(w.entries))
	for _, e := range w.entries {
		if w.filter.matches(e.Name) {
			shown = append(shown, e)
		}
	}
	w.shown = shown
	m.clampWork()
}

// workEmpty says why the level is empty: nothing here, or nothing matching.
func (m Model) workEmpty() string {
	if m.work.filter.on() {
		return fmt.Sprintf("nothing at this level matches %q", m.work.filter.text)
	}
	return "nothing here"
}

func workHints() []hint {
	return []hint{
		{"j/k", "down / up"}, {"↵", "open"},
		{"d", "delete to bin"}, {"f", "filter"},
		{"esc", "up a level"}, {"q", "quit"},
	}
}

func readingWorkHints() []hint {
	return []hint{
		{"j/k", "scroll"}, {"y", "copy the path"},
		{"esc", "back to the list"}, {"q", "quit"},
	}
}

// workInfo draws the header for whichever of the two screens is showing. What
// it draws comes from headerContent, which is also what sizes it: a header
// measured from one screen and drawn from another overflows its rows.
func (m Model) workInfo() string {
	facts, keys, glyphs := m.headerContent()
	return m.factsBlock(facts, keys, glyphs)
}

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

// Looking at a work product. Only the head is read: these files reach hundreds
// of megabytes, and a viewer that reads the file is a viewer that stalls the
// program. Data files are named rather than drawn — a database rendered as
// characters is a thousand screens of noise.

func (m Model) openWorkFile(entry artifacts.Entry) Model {
	peek, err := artifacts.Head(entry.Path, artifacts.PeekLimit)
	doc := &workDoc{entry: entry, peek: peek, err: err}
	m.rewrapWork(doc, m.contentWidth()-2)
	m.work.reading = doc
	m.work.notice, m.work.failed = "", false
	return m
}

func (m Model) readingWorkKey(key string) (Model, tea.Cmd) {
	doc := m.work.reading
	switch key {
	case "esc", "backspace", "h", "left":
		m.work.reading = nil
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
	case "y":
		// The path, because what you do with a work product braids will not
		// show you is open it in something that will.
		m.work.notice, m.work.failed = "copied: "+doc.entry.Path, false
		m.clampWorkDoc()
		return m, tea.SetClipboard(doc.entry.Path)
	}
	m.clampWorkDoc()
	return m, nil
}

func (m *Model) clampWorkDoc() {
	doc := m.work.reading
	if doc == nil {
		return
	}
	doc.offset = min(max(doc.offset, 0), max(len(doc.lines)-m.bodyHeight(), 0))
}

// rewrapWork lays the head out for a frame of this width, breaking lines
// rather than reflowing them: a line of JSON or a listing means what its
// columns say.
func (m Model) rewrapWork(doc *workDoc, width int) {
	if width < 8 {
		width = 8
	}
	if doc.width == width && doc.lines != nil {
		return
	}
	doc.width = width
	doc.lines = hardWrap(doc.peek.Text, width)
}

func (m Model) readingWorkFacts() []fact {
	doc := m.work.reading
	kind := "text"
	if doc.peek.Binary {
		kind = "data, not shown"
	}
	showing := "all of it"
	if doc.peek.Truncated() {
		showing = fmt.Sprintf("first %s", humanBytes(doc.peek.Read))
	}
	if doc.peek.Binary {
		showing = "nothing"
	}
	return []fact{
		{"File", doc.entry.Name},
		{"Size", humanBytes(doc.entry.Bytes)},
		{"Showing", showing},
		{"Kind", kind},
	}
}

func (m Model) renderWorkFile() string {
	doc := m.work.reading
	m.rewrapWork(doc, m.contentWidth()-2)
	m.clampWorkDoc()

	var out strings.Builder
	out.WriteString(m.workInfo())
	out.WriteString("\n\n")
	out.WriteString(m.panelTopTitled(truncate(doc.entry.Name, m.contentWidth()-6)))
	out.WriteString("\n")

	blank := repeat(" ", m.contentWidth())
	switch {
	case doc.err != nil:
		out.WriteString(m.framed(padRight(" "+m.theme.Empty.Render(doc.err.Error()), m.contentWidth())) + "\n")
		m.fill(&out, blank, m.bodyHeight()-1)
	case doc.peek.Binary:
		// Named, not drawn, and told what to do instead.
		lines := []string{
			m.theme.Empty.Render("this is data rather than text: " + humanBytes(doc.entry.Bytes) + " of it"),
			m.theme.Label.Render("y copies the path, to open it in something that can read it"),
		}
		for _, line := range lines {
			out.WriteString(m.framed(padRight(" "+line, m.contentWidth())) + "\n")
		}
		m.fill(&out, blank, m.bodyHeight()-len(lines))
	case doc.peek.Total == 0:
		out.WriteString(m.framed(padRight(" "+m.theme.Empty.Render("this file is empty"), m.contentWidth())) + "\n")
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
	if m.work.notice != "" {
		out.WriteString(" " + m.noticeStyle(m.work.failed).Render(truncate(m.work.notice, m.width-2)))
		return out.String()
	}
	out.WriteString(" " + m.theme.Label.Render(truncate(m.workReadingWhere(), m.width-2)))
	return out.String()
}

// workReadingWhere says how much of the file is on screen and where the rest
// of it is.
//
// How much comes first. The line is truncated to the frame, and a path deep in
// a job directory is long enough to push everything after it off the end —
// which would lose the one thing a reader of a partial file needs to know.
func (m Model) workReadingWhere() string {
	doc := m.work.reading
	if doc.peek.Truncated() && !doc.peek.Binary {
		return fmt.Sprintf("%s read of %s, the rest is on disk · %s",
			humanBytes(doc.peek.Read), humanBytes(doc.peek.Total), doc.entry.Path)
	}
	return doc.entry.Path
}

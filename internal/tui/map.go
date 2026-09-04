package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Ashes47/braids/internal/core/graph"
	"github.com/Ashes47/braids/internal/core/index"
	"github.com/Ashes47/braids/internal/core/model"
	"github.com/Ashes47/braids/internal/core/trash"
)

// Column widths for the right-hand block. Everything left of it flexes.
const (
	forkWidth    = 7
	projectWidth = 10
	msgsWidth    = 7
	sizeWidth    = 8
	workWidth    = 9
	ageWidth     = 5
	statusWidth  = 10
)

// row is one lane placed on screen together with the tree art leading to it.
type row struct {
	node   *graph.Node
	prefix string
}

// Options configure the map. Source and IndexPath appear in the facts block,
// so the screen always says what it is looking at.
type Options struct {
	ASCII     bool
	Source    string
	IndexPath string
	// LoadSpine reduces one lane to its spine. Passing it as a function keeps
	// the views free of any dependency on the index.
	LoadSpine func(laneID string) ([]graph.Segment, error)
	// WorkspaceOK reports whether a conversation can take a workspace branch,
	// explaining why not when it cannot.
	WorkspaceOK func(laneID string) error
	// LoadCompactions lists where a conversation was compacted.
	LoadCompactions func(laneID string) ([]index.CompactionRow, error)
	// LoadSubagents lists the conversations a lane spawned and collapsed into
	// single tool calls.
	LoadSubagents func(laneID string) ([]index.SubagentRow, error)
	// Promote turns a subagent into a conversation of its own, returning the
	// new lane's ID.
	Promote func(laneID, agentID string) (string, error)
	// LoadAgentSpine reads a subagent's own transcript, so it can be looked at
	// before any decision is made about it.
	LoadAgentSpine func(laneID, agentID string) ([]graph.Segment, error)
	// Branch cuts a new conversation from a lane at a turn, returning the new
	// lane's ID. Nil disables branching, which is what a read-only Source gets.
	Branch func(laneID string, turn int, name string, workspace bool) (string, error)
	// Origins is recorded branch provenance, preferred over inference.
	Origins map[string]model.Origin
	// Names are the names the user has given conversations, which replace
	// whatever the harness called them.
	Names map[string]string
	// Refresh brings the index up to date and rebuilds the forest. Called
	// after a branch so the new lane appears at once instead of after a
	// remembered command.
	Refresh func() (*graph.Forest, error)
	// Archived is the set of conversations put out of the way. They stay
	// searchable and reachable, but do not clutter the map.
	Archived map[string]bool
	// Archive puts a conversation out of the way, or brings it back.
	Archive func(laneID string, archived bool) error
	// Rename gives a conversation a name of your own. An empty name restores
	// whatever the harness called it.
	Rename func(laneID, name string) error
	// Delete moves a conversation's files to the bin, returning how much was
	// reclaimed. Nil disables deleting.
	Delete func(laneID string) (int64, error)
	// DeleteWork moves a conversation's work products to the bin, leaving the
	// conversation itself alone.
	DeleteWork func(laneID string) (int64, error)
	// LoadBin lists what has been deleted and not yet expired.
	LoadBin func() ([]trash.Entry, error)
	// Restore brings a deleted conversation back.
	Restore func(entryID string) error
	// Purge removes a deleted conversation for good.
	Purge func(entryID string) error
	// Changes signals that transcripts moved on disk. With it the map follows
	// live sessions; without it the map is a snapshot.
	Changes <-chan struct{}
	// ResumeCommand builds the shell command that continues a conversation.
	ResumeCommand func(laneID string) (string, error)
	// Spawn opens a terminal already resumed into a conversation. Nil when the
	// user has configured no way to launch one, which is the default: braids
	// copies the command instead of guessing at their terminal.
	Spawn func(laneID string) error
	// Terminal names what Spawn drives, so the map can say so instead of
	// claiming a window opened somewhere unspecified.
	Terminal string
	// Search runs a full-text query over every indexed message and tool call.
	// An empty scope searches everywhere; a lane ID narrows to one.
	Search func(query, scope string) ([]index.Hit, error)
}

// changedMsg says transcripts moved and the map should catch up.
type changedMsg struct{}

// mode is which screen the map is showing.
type mode int

const (
	mapMode mode = iota
	spineMode
	searchMode
	binMode
)

// Model is the Map: every conversation and every branch as a single forest.
type Model struct {
	theme          Theme
	ascii          bool
	source         string
	indexPath      string
	now            func() time.Time
	loadSpine      func(string) ([]graph.Segment, error)
	loadAgents     func(string) ([]index.SubagentRow, error)
	loadSeams      func(string) ([]index.CompactionRow, error)
	workspaceOK    func(string) error
	promote        func(string, string) (string, error)
	loadAgentSpine func(string, string) ([]graph.Segment, error)
	branch         func(string, int, string, bool) (string, error)
	refresh        func() (*graph.Forest, error)
	changes        <-chan struct{}
	resumeCmd      func(string) (string, error)
	spawn          func(string) error
	terminal       string
	searchFn       func(string, string) ([]index.Hit, error)
	archived       map[string]bool
	archiveFn      func(string, bool) error
	renameFn       func(string, string) error
	// naming is the rename field, opened with r on a conversation.
	naming       filterInput
	deleteFn     func(string) (int64, error)
	deleteWorkFn func(string) (int64, error)
	loadBin      func() ([]trash.Entry, error)
	restoreFn    func(string) error
	purgeFn      func(string) error
	// showArchived reveals what has been put away, so it can be brought back.
	showArchived bool

	notice string
	failed bool

	forest   *graph.Forest
	mode     mode
	returnTo mode
	spine    *spineState
	stack    []*spineState
	search   *searchState
	bin      *binState

	all     []row
	visible []row
	filter  filterInput

	// forestHas is every lane the map knows about, used to count how many
	// archived conversations are being hidden.
	forestHas map[string]struct{}

	// Columns that earn their space only sometimes: a fork column is dead
	// weight until a fork exists, and a project column until there is more
	// than one project. Ambiguous titles get their lane ID appended.
	showFork    bool
	showProject bool
	showWork    bool
	ambiguous   map[string]bool

	cursor int
	offset int
	width  int
	height int
}

// NewModel builds the Map over a forest. The theme starts dark and is corrected
// as soon as the terminal reports its real background.
func NewModel(f *graph.Forest, opts Options) Model {
	m := Model{
		theme:          NewTheme(true, opts.ASCII),
		ascii:          opts.ASCII,
		source:         opts.Source,
		indexPath:      opts.IndexPath,
		loadSpine:      opts.LoadSpine,
		loadAgents:     opts.LoadSubagents,
		loadSeams:      opts.LoadCompactions,
		workspaceOK:    opts.WorkspaceOK,
		promote:        opts.Promote,
		loadAgentSpine: opts.LoadAgentSpine,
		branch:         opts.Branch,
		refresh:        opts.Refresh,
		changes:        opts.Changes,
		resumeCmd:      opts.ResumeCommand,
		spawn:          opts.Spawn,
		terminal:       opts.Terminal,
		searchFn:       opts.Search,
		archived:       opts.Archived,
		archiveFn:      opts.Archive,
		renameFn:       opts.Rename,
		deleteFn:       opts.Delete,
		deleteWorkFn:   opts.DeleteWork,
		loadBin:        opts.LoadBin,
		restoreFn:      opts.Restore,
		purgeFn:        opts.Purge,
		now:            time.Now,
		width:          80,
		height:         24,
	}
	m.forest = f
	m.all = layout(f, m.theme.Glyphs, nil)
	m.measure()
	m.apply()
	return m
}

// adopt swaps in a rebuilt forest, holding the reader's place: the selected
// lane stays selected, and an open spine keeps reading the same conversation.
func (m Model) adopt(f *graph.Forest) Model {
	selected := ""
	if m.cursor < len(m.visible) {
		selected = m.visible[m.cursor].node.Lane.ID
	}
	m.forest = f
	m.all = layout(f, m.theme.Glyphs, nil)
	m.measure()
	m.apply()
	for i, r := range m.visible {
		if r.node.Lane.ID == selected {
			m.cursor = i
			break
		}
	}
	m.clamp()

	if m.spine != nil {
		if node, ok := f.ByID[m.spine.lane.ID]; ok {
			m.spine.node = node
			m.spine.lane = node.Lane
			m.spine.build()
			m.clampSpine()
		}
	}
	return m
}

// measure decides which optional columns to draw.
func (m *Model) measure() {
	titles := make(map[string]int, len(m.all))
	projects := make(map[string]struct{}, 4)
	for _, r := range m.all {
		if r.node.ParentID != "" {
			m.showFork = true
		}
		titles[r.node.Lane.Title]++
		projects[r.node.Lane.Project] = struct{}{}
	}
	m.showProject = len(projects) > 1
	for _, r := range m.all {
		if r.node.Lane.ArtifactBytes > 0 {
			m.showWork = true
			break
		}
	}
	m.forestHas = make(map[string]struct{}, len(m.all))
	for _, r := range m.all {
		m.forestHas[r.node.Lane.ID] = struct{}{}
	}
	m.ambiguous = make(map[string]bool)
	for title, n := range titles {
		if n > 1 && title != "" {
			m.ambiguous[title] = true
		}
	}
}

// Init asks the terminal for its background colour and, when watching, starts
// listening for changes on disk.
func (m Model) Init() tea.Cmd {
	if m.changes == nil {
		return tea.RequestBackgroundColor
	}
	return tea.Batch(tea.RequestBackgroundColor, awaitChange(m.changes))
}

// awaitChange blocks one command on the watcher. Re-armed after every change,
// it keeps exactly one listener alive for the life of the program.
func awaitChange(ch <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		if _, ok := <-ch; !ok {
			return nil
		}
		return changedMsg{}
	}
}

// Update handles terminal events.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		m.theme = NewTheme(msg.IsDark(), m.ascii)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clamp()
		m.clampSpine()
		m.clampBin()
	case tea.PasteMsg:
		return m.pasted(msg.Content), nil
	case changedMsg:
		return m.catchUp(), awaitChange(m.changes)
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

// pasted routes pasted text to whichever field is taking input. Without this a
// paste is dropped silently, which is worse than not supporting it.
func (m Model) pasted(text string) Model {
	switch {
	case m.mode == searchMode && m.search != nil:
		m.search.input.paste(text)
		m.runSearch()
		m.clampSearch()
	case m.mode == spineMode && m.spine != nil && m.spine.naming.active:
		m.spine.naming.paste(text)
	case m.mode == spineMode && m.spine != nil && m.spine.filter.active:
		m.spine.filter.paste(text)
		m.spine.apply()
		m.clampSpine()
	case m.mode == mapMode && m.naming.active:
		m.naming.paste(text)
	case m.filter.active:
		m.filter.paste(text)
		m.apply()
		m.clamp()
	}
	return m
}

// catchUp re-syncs after something moved on disk. A failure is left silent:
// transcripts are written continuously, so a half-written file resolves itself
// on the next change rather than deserving an alarm.
func (m Model) catchUp() Model {
	if m.refresh == nil {
		return m
	}
	forest, err := m.refresh()
	if err != nil {
		return m
	}
	return m.adopt(forest)
}

// View renders whichever screen is active, full-screen.
func (m Model) View() tea.View {
	switch {
	case m.mode == binMode && m.bin != nil:
		return tea.View{Content: m.renderBin(), AltScreen: true}
	case m.mode == searchMode && m.search != nil:
		return tea.View{Content: m.renderSearch(), AltScreen: true}
	case m.mode == spineMode && m.spine != nil:
		return tea.View{Content: m.renderSpine(), AltScreen: true}
	default:
		return tea.View{Content: m.render(), AltScreen: true}
	}
}

// normalizeKey folds the spellings a terminal may use for the same key. Return
// arrives as CR on most terminals but as LF on some configurations, and only
// the CR form is named "enter".
func normalizeKey(k string) string {
	switch k {
	case "\n", "return", "ctrl+m":
		return "enter"
	case "ctrl+j":
		return "enter"
	}
	return k
}

func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := normalizeKey(msg.String())
	if key == "ctrl+c" || (key == "q" && !m.editing()) {
		return m, tea.Quit
	}
	if m.mode == searchMode {
		return m.searchKey(key), nil
	}
	if m.mode == binMode {
		return m.binKey(key), nil
	}
	if m.mode == mapMode && m.naming.active {
		return m.renameKey(key), nil
	}
	if key == "u" && m.loadBin != nil {
		return m.openBin(), nil
	}
	if key == "/" && m.searchFn != nil {
		return m.openSearch(), nil
	}
	if m.mode == spineMode {
		return m.spineKey(key)
	}
	if m.filter.key(key) {
		m.apply()
		m.clamp()
		return m, nil
	}
	switch key {
	case "enter", "l", "right":
		return m.openSpine(), nil
	case "f":
		m.filter.active = true
	case "r":
		return m.startRename(), nil
	case "a":
		return m.toggleArchive(), nil
	case "A":
		m.showArchived = !m.showArchived
		m.apply()
		m.clamp()
	case "d":
		return m.deleteLane(), nil
	case "D":
		return m.deleteWork(), nil
	case "n":
		m.cursor = m.nextWaiting(m.cursor, 1)
	case "N":
		m.cursor = m.nextWaiting(m.cursor, -1)
	case "y":
		return m.copyResume()
	case "o":
		return m.openTerminal()
	case "j", "down":
		m.cursor = wrap(m.cursor, 1, len(m.visible))
	case "k", "up":
		m.cursor = wrap(m.cursor, -1, len(m.visible))
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = len(m.visible) - 1
	case "ctrl+d", "pgdown":
		m.cursor += m.bodyHeight() / 2
	case "ctrl+u", "pgup":
		m.cursor -= m.bodyHeight() / 2
	}
	m.clamp()
	return m, nil
}

// startRename opens the name field on the selected conversation. Names live in
// braids' own sidecar, so renaming never touches a transcript and can always be
// undone by clearing the field.
func (m Model) startRename() Model {
	if m.renameFn == nil || m.cursor >= len(m.visible) {
		return m.withNotice("nothing to rename here", true)
	}
	m.naming = filterInput{active: true, text: m.visible[m.cursor].node.Lane.Title}
	m.notice = ""
	return m
}

func (m Model) renameKey(key string) Model {
	switch key {
	case "esc":
		m.naming = filterInput{}
	case "enter":
		return m.commitRename()
	default:
		m.naming.edit(key)
	}
	return m
}

func (m Model) commitRename() Model {
	lane := m.visible[m.cursor].node.Lane.ID
	name := strings.TrimSpace(m.naming.text)
	m.naming = filterInput{}

	if err := m.renameFn(lane, name); err != nil {
		return m.withNotice(err.Error(), true)
	}
	m = m.catchUp()
	if name == "" {
		return m.withNotice("name cleared · back to what the harness called it", false)
	}
	return m.withNotice("renamed to "+name, false)
}

// toggleArchive puts a conversation out of the way, or brings it back.
// Archiving is the gesture for most tidying: it is instant, reversible, and
// leaves the conversation searchable, so nothing has to be deleted to get a
// map that reflects what is being worked on.
func (m Model) toggleArchive() Model {
	lane, ok := m.selectedLane()
	if !ok || m.archiveFn == nil {
		return m.withNotice("nothing to archive here", true)
	}
	if m.archived == nil {
		m.archived = map[string]bool{}
	}
	archived := !m.archived[lane]
	if err := m.archiveFn(lane, archived); err != nil {
		return m.withNotice(err.Error(), true)
	}
	if archived {
		m.archived[lane] = true
	} else {
		delete(m.archived, lane)
	}
	m.apply()
	m.clamp()
	if archived {
		return m.withNotice("archived "+shortID(lane)+" · A shows archived, a brings it back", false)
	}
	return m.withNotice("unarchived "+shortID(lane), false)
}

// deleteLane moves a conversation's files to the bin.
//
// It is refused while a conversation is being written to: deleting a session
// mid-turn is the one accident here worth preventing outright. It is never
// refused for having children, because a fork carries its own copy of the
// prefix it shares — deleting a parent cannot break one.
func (m Model) deleteLane() Model {
	lane, ok := m.selectedLane()
	if !ok || m.deleteFn == nil {
		return m.withNotice("nothing to delete here", true)
	}
	info := m.visible[m.cursor].node.Lane
	if stateOf(info, m.now()) == stateWorking {
		return m.withNotice("that conversation is still running — stop it first", true)
	}
	bytes, err := m.deleteFn(lane)
	if err != nil {
		return m.withNotice(err.Error(), true)
	}
	m = m.catchUp()
	return m.withNotice(fmt.Sprintf("deleted %s · %s reclaimed · u to recover · children unaffected",
		shortID(lane), humanBytes(bytes)), false)
}

// deleteWork discards a conversation's work products and keeps the
// conversation. Scratch files and job records are usually most of what a
// session occupies — 3.5 GB against 365 MB of transcript here — and letting
// them go costs nothing that can be read again.
func (m Model) deleteWork() Model {
	lane, ok := m.selectedLane()
	if !ok || m.deleteWorkFn == nil {
		return m.withNotice("nothing here has work products", true)
	}
	bytes, err := m.deleteWorkFn(lane)
	if err != nil {
		return m.withNotice(err.Error(), true)
	}
	m = m.catchUp()
	return m.withNotice(fmt.Sprintf("discarded %s of work products · the conversation is untouched · u to recover",
		humanBytes(bytes)), false)
}

// selectedLane is the conversation under the cursor on whichever screen is up.
func (m Model) selectedLane() (string, bool) {
	if m.mode == spineMode && m.spine != nil {
		if m.spine.agentOf != "" {
			// A subagent is not a session; there is nothing to resume until it
			// has been promoted.
			return "", false
		}
		return m.spine.lane.ID, true
	}
	if m.cursor < len(m.visible) {
		return m.visible[m.cursor].node.Lane.ID, true
	}
	return "", false
}

// copyResume puts the resume command on the clipboard. It goes through the
// terminal's own copy escape, so it works over SSH and needs no helper binary.
func (m Model) copyResume() (tea.Model, tea.Cmd) {
	lane, ok := m.selectedLane()
	if !ok || m.resumeCmd == nil {
		return m.withNotice("nothing to resume here — promote the agent first", true), nil
	}
	command, err := m.resumeCmd(lane)
	if err != nil {
		return m.withNotice(err.Error(), true), nil
	}
	return m.withNotice("copied: "+command, false), tea.SetClipboard(command)
}

// openTerminal launches a session, or explains how to make that possible.
func (m Model) openTerminal() (tea.Model, tea.Cmd) {
	lane, ok := m.selectedLane()
	if !ok {
		return m, nil
	}
	if m.spawn == nil {
		updated, cmd := m.copyResume()
		return updated.(Model).withNotice(
			"no terminal configured — command copied; set BRAIDS_SPAWN to open one", true), cmd
	}
	if err := m.spawn(lane); err != nil {
		return m.withNotice("could not open a terminal — "+err.Error(), true), nil
	}
	return m.withNotice(fmt.Sprintf("opened %s in %s", shortID(lane), m.terminal), false), nil
}

// withNotice records a one-line outcome for whichever screen is showing.
func (m Model) withNotice(text string, failed bool) Model {
	if m.mode == spineMode && m.spine != nil {
		m.spine.notice, m.spine.failed = text, failed
		return m
	}
	m.notice, m.failed = text, failed
	return m
}

// editing reports whether a text field currently owns typed keys, so that "q"
// types a letter instead of quitting.
func (m Model) editing() bool {
	switch {
	case m.mode == searchMode:
		return true
	case m.mode == binMode:
		return false
	case m.mode == mapMode && m.naming.active:
		return true
	case m.mode == spineMode && m.spine != nil:
		return m.spine.filter.active || m.spine.naming.active
	default:
		return m.filter.active
	}
}

// apply narrows the visible rows to those matching the filter. Filtering drops
// the tree art: a partial forest with dangling connectors reads as corruption.
func (m *Model) apply() {
	held := ""
	if m.cursor >= 0 && m.cursor < len(m.visible) {
		held = m.visible[m.cursor].node.Lane.ID
	}

	switch {
	case m.filter.on():
		// Filtering drops the tree art: a partial forest with connectors
		// leading to rows that are not there reads as corruption.
		m.visible = nil
		for _, r := range m.all {
			l := r.node.Lane
			if m.archived[l.ID] && !m.showArchived {
				continue
			}
			if m.filter.matches(l.Title + " " + l.Project + " " + l.ID) {
				m.visible = append(m.visible, row{node: r.node})
			}
		}
	case m.hidingAny():
		// Hiding archived rows keeps the tree, redrawn without them: a branch
		// whose parent is archived is shown at the level the parent held.
		m.visible = layout(m.forest, m.theme.Glyphs, func(n *graph.Node) bool {
			return !m.archived[n.Lane.ID]
		})
	default:
		m.visible = m.all
	}
	// Follow the selected lane across the change, so clearing a filter leaves
	// you where you were rather than back at the top.
	m.cursor = 0
	for i, r := range m.visible {
		if r.node.Lane.ID == held {
			m.cursor = i
			break
		}
	}
}

func (m *Model) clamp() {
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	h := m.bodyHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// wrap moves a cursor by delta through n rows, running off either end into the
// other. Only single-step moves wrap: a half-page jump that silently landed at
// the far end of a long conversation would lose the reader entirely.
func wrap(cursor, delta, n int) int {
	if n <= 0 {
		return 0
	}
	return ((cursor+delta)%n + n) % n
}

// bodyHeight is the terminal height minus the chrome.
func (m Model) bodyHeight() int {
	h := m.height - m.chromeHeight()
	if h < 1 {
		return 1
	}
	return h
}

// contentWidth is the usable width inside the panel borders.
func (m Model) contentWidth() int {
	w := m.width - 2
	if w < 20 {
		return 20
	}
	return w
}

func (m Model) render() string {
	var b strings.Builder
	b.WriteString(m.info())
	b.WriteString("\n\n")
	b.WriteString(m.panelTop())
	b.WriteString("\n")
	b.WriteString(m.framed(m.columns()))
	b.WriteString("\n")

	blank := strings.Repeat(" ", m.contentWidth())
	if len(m.visible) == 0 {
		empty := m.theme.Empty.Render(m.emptyMessage())
		b.WriteString(m.framed(padRight(" "+empty, m.contentWidth()+lipgloss.Width(empty)-lipgloss.Width(m.emptyMessage()))))
		b.WriteString("\n")
		for range m.bodyHeight() - 1 {
			b.WriteString(m.framed(blank) + "\n")
		}
	} else {
		drawn := 0
		end := min(m.offset+m.bodyHeight(), len(m.visible))
		for i := m.offset; i < end && drawn < m.bodyHeight(); i++ {
			b.WriteString(m.framed(m.renderRow(m.visible[i], i == m.cursor)))
			b.WriteString("\n")
			drawn++
			// The name field opens on the conversation it renames, not in a
			// corner of the screen.
			if m.naming.active && i == m.cursor && drawn < m.bodyHeight() {
				b.WriteString(m.framed(m.renamePrompt()))
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
	b.WriteString(m.statusLine())
	return b.String()
}

func (m Model) emptyMessage() string {
	if m.filter.on() {
		return fmt.Sprintf("nothing matches %q", m.filter.text)
	}
	return "no conversations indexed yet — run: braids index"
}

// waitingCount is how many conversations are owed something by a person.
func (m Model) waitingCount() int {
	n := 0
	for _, r := range m.all {
		if waiting(r.node.Lane, m.now()) {
			n++
		}
	}
	return n
}

// nextWaiting finds the next conversation owed a reply, wrapping around.
func (m Model) nextWaiting(from, step int) int {
	for i := 1; i <= len(m.visible); i++ {
		at := (from + i*step + len(m.visible)*i) % len(m.visible)
		if waiting(m.visible[at].node.Lane, m.now()) {
			return at
		}
	}
	return from
}

// rowLayout is which columns the map draws. Both the header and the rows are
// built from it, so they cannot drift apart — the two width bugs this screen
// has already had were exactly that drift.
type rowLayout struct {
	fork, project, turns, size, work, status bool
	right                                    int // width of everything after the title
}

// layoutFor drops columns from the right as the terminal narrows, keeping the
// name and the age: what it is, and whether it is stale.
func (m Model) layoutFor() rowLayout {
	l := rowLayout{
		fork: m.showFork, project: m.showProject,
		turns: true, size: true, work: m.showWork, status: true,
	}
	width := func(l rowLayout) int {
		n := ageWidth
		for on, w := range map[*bool]int{
			&l.fork: forkWidth, &l.project: projectWidth, &l.turns: msgsWidth,
			&l.size: sizeWidth, &l.work: workWidth, &l.status: statusWidth,
		} {
			if *on {
				n += w + 2
			}
		}
		return n
	}
	// Give up the least informative column first.
	for _, drop := range []*bool{&l.size, &l.work, &l.status, &l.project, &l.turns, &l.fork} {
		if m.contentWidth()-4-width(l) >= minTitleWidth {
			break
		}
		*drop = false
	}
	l.right = width(l)
	return l
}

// minTitleWidth is the least a conversation's name may be squeezed to before
// columns start being dropped instead.
const minTitleWidth = 16

// renamePrompt is the inline name field, drawn under the conversation it names.
func (m Model) renamePrompt() string {
	g := m.theme.Glyphs
	label := "name: "
	hint := "enter save · empty to clear · esc cancel"
	width := m.contentWidth() - 4 - lipgloss.Width(g.Last) - lipgloss.Width(label) -
		lipgloss.Width(hint) - 2
	if width < 8 {
		width = 8
	}
	return " " + m.theme.Rail.Render("  "+g.Last) + " " +
		m.theme.Accent.Render(label) +
		m.theme.Value.Render(padRight(truncate(m.naming.text, width)+"▏", width+1)) + " " +
		m.theme.Label.Render(hint)
}

// renderRow draws one lane. A selected row is painted as a single flat band so
// that no nested style can tear the background; an unselected row gets the
// per-segment colouring.
func (m Model) renderRow(r row, selected bool) string {
	plain, styled := m.rowParts(r)
	if selected {
		return m.theme.Selected.Width(m.contentWidth()).Render(plain)
	}
	return styled
}

func (m Model) rowParts(r row) (plain, styled string) {
	g := m.theme.Glyphs
	lane := r.node.Lane

	state := stateOf(lane, m.now())
	glyphStyle := m.styleFor(lane, state)
	mark := g.Lane
	if m.archived[lane.ID] {
		// Archived rows read as set aside rather than active, even while shown.
		mark, glyphStyle = g.Archived, m.theme.Faint
	}

	title := lane.Title
	if title == "" {
		title = lane.ID
	}

	// A lane ID only appears when the title alone cannot identify the lane.
	suffix := ""
	if m.ambiguous[lane.Title] {
		suffix = "  " + shortID(lane.ID)
	}

	layout := m.layoutFor()
	var rightPlain, rightStyled string
	if layout.fork {
		fork := ""
		if r.node.ParentID != "" {
			fork = fmt.Sprintf("%s t%d", g.Fork, r.node.ForkSeq)
		}
		cell := padLeft(fork, forkWidth)
		rightPlain += cell + "  "
		rightStyled += m.theme.Faint.Render(cell) + "  "
	}
	if layout.project {
		cell := padRight(truncate(lane.Project, projectWidth), projectWidth)
		rightPlain += cell + "  "
		rightStyled += m.theme.Faint.Render(cell) + "  "
	}
	if layout.turns {
		cell := padLeft(fmt.Sprintf("%d", lane.Messages), msgsWidth)
		rightPlain += cell + "  "
		rightStyled += m.theme.Value.Render(cell) + "  "
	}
	if layout.size {
		cell := padLeft(humanBytes(lane.Size), sizeWidth)
		rightPlain += cell + "  "
		rightStyled += m.theme.Faint.Render(cell) + "  "
	}
	if layout.work {
		cell := padLeft(orBlank(lane.ArtifactBytes), workWidth)
		rightPlain += cell + "  "
		// Work products are worth noticing when they dwarf the conversation,
		// which is usually, so they are not drawn as quietly as a byte count.
		rightStyled += m.theme.Dim.Render(cell) + "  "
	}
	age := padLeft(humanAge(m.now().Sub(lane.Updated)), ageWidth)
	rightPlain += age
	rightStyled += m.theme.Dim.Render(age)
	if layout.status {
		cell := padLeft(string(state), statusWidth)
		rightPlain += "  " + cell
		rightStyled += "  " + m.styleFor(lane, state).Render(cell)
	}

	// row = " " + prefix + glyph + " " + title + suffix + " " + right,
	// so the fixed cost either side of the title is 4 cells.
	titleWidth := m.contentWidth() - 4 - lipgloss.Width(r.prefix) - layout.right - lipgloss.Width(suffix)
	if titleWidth < 4 {
		titleWidth = 4
	}
	name := padRight(truncate(title, titleWidth), titleWidth)

	rail := ""
	if r.prefix != "" {
		rail = m.theme.Rail.Render(r.prefix)
	}
	plain = " " + r.prefix + mark + " " + name + suffix + " " + rightPlain
	styled = " " + rail + glyphStyle.Render(mark) + " " +
		m.theme.Title.Render(name) + m.theme.Faint.Render(suffix) + " " + rightStyled
	return plain, styled
}

// columns draws the k9s-style header row above the lanes.
func (m Model) columns() string {
	layout := m.layoutFor()
	right := ""
	if layout.fork {
		right += padLeft("FORK", forkWidth) + "  "
	}
	if layout.project {
		right += padRight("PROJECT", projectWidth) + "  "
	}
	if layout.turns {
		right += padLeft("TURNS", msgsWidth) + "  "
	}
	if layout.size {
		right += padLeft("SIZE", sizeWidth) + "  "
	}
	if layout.work {
		right += padLeft("WORK", workWidth) + "  "
	}
	right += padLeft("AGE", ageWidth)
	if layout.status {
		right += "  " + padLeft("STATUS", statusWidth)
	}

	nameWidth := m.contentWidth() - 2 - lipgloss.Width(right)
	if nameWidth < 4 {
		nameWidth = 4
	}
	return m.theme.Column.Render(" " + padRight(" CONVERSATION", nameWidth) + " " + right)
}

// shortID is enough of a lane ID to tell two same-titled conversations apart.
func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// layout walks the forest into display rows, drawing the connectors that show
// which lane forked from which.
//
// show may exclude nodes. An excluded node's children take its place at its own
// level rather than disappearing with it, so hiding a conversation never
// removes a branch that was made from it — nor leaves a connector pointing at
// a row that is not there.
func layout(f *graph.Forest, g Glyphs, show func(*graph.Node) bool) []row {
	var out []row
	var walk func(nodes []*graph.Node, ancestors string, depth int)
	walk = func(nodes []*graph.Node, ancestors string, depth int) {
		visible := shown(nodes, show)
		for i, n := range visible {
			last := i == len(visible)-1
			prefix := ""
			if depth > 0 {
				prefix = ancestors + g.Branch
				if last {
					prefix = ancestors + g.Last
				}
			}
			out = append(out, row{node: n, prefix: prefix})

			childAncestors := ancestors
			if depth > 0 {
				childAncestors += g.Pipe
				if last {
					childAncestors = ancestors + g.Blank
				}
			}
			walk(n.Children, childAncestors, depth+1)
		}
	}
	walk(f.Roots, "", 0)
	return out
}

// shown resolves a level to the nodes that are actually drawn, replacing each
// excluded node with its own visible descendants, in place.
func shown(nodes []*graph.Node, show func(*graph.Node) bool) []*graph.Node {
	if show == nil {
		return nodes
	}
	var out []*graph.Node
	for _, n := range nodes {
		if show(n) {
			out = append(out, n)
			continue
		}
		out = append(out, shown(n.Children, show)...)
	}
	return out
}

// hidingAny reports whether any conversation on the map is being held back.
func (m Model) hidingAny() bool {
	if m.showArchived {
		return false
	}
	for id := range m.archived {
		if _, ok := m.forestHas[id]; ok {
			return true
		}
	}
	return false
}

// oneLine lives in spineview.go.

// orBlank leaves an empty cell rather than printing a zero, so the eye lands
// only on conversations that actually hold something.
func orBlank(n int64) string {
	if n == 0 {
		return ""
	}
	return humanBytes(n)
}

// humanBytes renders a transcript size compactly.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f kB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// homeDir is a variable so tests can pin it.
var homeDir = os.UserHomeDir

// humanAge renders a duration the way someone scanning for staleness reads it.
func humanAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// spread pushes left and right to opposite edges of the given width.
func spread(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap >= 1 {
		return left + strings.Repeat(" ", gap) + right
	}
	// Nothing fits: keep the left side, which carries the facts, and truncate
	// rather than run past the edge of the terminal.
	return truncate(left, width)
}

// truncate cuts to a display width, measuring cells rather than runes so that
// wide characters cannot overflow a column.
func truncate(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		w := lipgloss.Width(string(r))
		if used+w > width-1 {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String() + "…"
}

func padRight(s string, width int) string {
	if gap := width - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

func padLeft(s string, width int) string {
	if gap := width - lipgloss.Width(s); gap > 0 {
		return strings.Repeat(" ", gap) + s
	}
	return s
}

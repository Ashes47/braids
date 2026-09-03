package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Ashes47/braids/internal/core/graph"
	"github.com/Ashes47/braids/internal/core/model"
)

// activeWindow is how recently a lane must have changed to read as alive.
// Until hooks land, recency is the only liveness braids can honestly claim.
const activeWindow = 10 * time.Minute

// Column widths for the right-hand block. Everything left of it flexes.
const (
	forkWidth    = 7
	projectWidth = 10
	msgsWidth    = 7
	sizeWidth    = 8
	ageWidth     = 5
	statusWidth  = 6
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
	// Branch cuts a new conversation from a lane at a turn, returning the new
	// lane's ID. Nil disables branching, which is what a read-only Source gets.
	Branch func(laneID string, turn int, name string) (string, error)
	// Origins is recorded branch provenance, preferred over inference.
	Origins map[string]model.Origin
	// Refresh brings the index up to date and rebuilds the forest. Called
	// after a branch so the new lane appears at once instead of after a
	// remembered command.
	Refresh func() (*graph.Forest, error)
}

// mode is which screen the map is showing.
type mode int

const (
	mapMode mode = iota
	spineMode
)

// Model is the Map: every conversation and every branch as a single forest.
type Model struct {
	theme     Theme
	ascii     bool
	source    string
	indexPath string
	now       func() time.Time
	loadSpine func(string) ([]graph.Segment, error)
	branch    func(string, int, string) (string, error)
	refresh   func() (*graph.Forest, error)

	mode  mode
	spine *spineState
	stack []*spineState

	all     []row
	visible []row
	filter  filterInput

	// Columns that earn their space only sometimes: a fork column is dead
	// weight until a fork exists, and a project column until there is more
	// than one project. Ambiguous titles get their lane ID appended.
	showFork    bool
	showProject bool
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
		theme:     NewTheme(true, opts.ASCII),
		ascii:     opts.ASCII,
		source:    opts.Source,
		indexPath: opts.IndexPath,
		loadSpine: opts.LoadSpine,
		branch:    opts.Branch,
		refresh:   opts.Refresh,
		now:       time.Now,
		width:     80,
		height:    24,
	}
	m.all = layout(f, m.theme.Glyphs)
	m.visible = m.all
	m.measure()
	return m
}

// adopt swaps in a rebuilt forest, holding the reader's place: the selected
// lane stays selected, and an open spine keeps reading the same conversation.
func (m Model) adopt(f *graph.Forest) Model {
	selected := ""
	if m.cursor < len(m.visible) {
		selected = m.visible[m.cursor].node.Lane.ID
	}
	m.all = layout(f, m.theme.Glyphs)
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
	m.ambiguous = make(map[string]bool)
	for title, n := range titles {
		if n > 1 && title != "" {
			m.ambiguous[title] = true
		}
	}
}

// Init asks the terminal for its background colour so the palette can adapt.
func (m Model) Init() tea.Cmd { return tea.RequestBackgroundColor }

// Update handles terminal events.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		m.theme = NewTheme(msg.IsDark(), m.ascii)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clamp()
		m.clampSpine()
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

// View renders whichever screen is active, full-screen.
func (m Model) View() tea.View {
	if m.mode == spineMode && m.spine != nil {
		return tea.View{Content: m.renderSpine(), AltScreen: true}
	}
	return tea.View{Content: m.render(), AltScreen: true}
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
	if m.mode == spineMode {
		return m.spineKey(key), nil
	}
	if m.filter.key(key) {
		m.apply()
		m.clamp()
		return m, nil
	}
	switch key {
	case "enter", "l", "right":
		return m.openSpine(), nil
	case "j", "down":
		m.cursor++
	case "k", "up":
		m.cursor--
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

// editing reports whether a text field currently owns typed keys, so that "q"
// types a letter instead of quitting.
func (m Model) editing() bool {
	if m.mode == spineMode && m.spine != nil {
		return m.spine.filter.active || m.spine.naming.active
	}
	return m.filter.active
}

// apply narrows the visible rows to those matching the filter. Filtering drops
// the tree art: a partial forest with dangling connectors reads as corruption.
func (m *Model) apply() {
	if !m.filter.on() {
		m.visible = m.all
		return
	}
	m.visible = nil
	for _, r := range m.all {
		l := r.node.Lane
		if m.filter.matches(l.Title + " " + l.Project + " " + l.ID) {
			m.visible = append(m.visible, row{node: r.node})
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

// bodyHeight is the terminal height minus the chrome.
func (m Model) bodyHeight() int {
	h := m.height - chromeHeight
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
		end := min(m.offset+m.bodyHeight(), len(m.visible))
		for i := m.offset; i < end; i++ {
			b.WriteString(m.framed(m.renderRow(m.visible[i], i == m.cursor)))
			b.WriteString("\n")
		}
		for range m.bodyHeight() - (end - m.offset) {
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

func (m Model) activeCount() int {
	n := 0
	for _, r := range m.all {
		if m.isActive(r.node) {
			n++
		}
	}
	return n
}

func (m Model) isActive(n *graph.Node) bool {
	return m.now().Sub(n.Lane.Updated) < activeWindow
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

	glyphStyle := m.theme.Faint
	if m.isActive(r.node) {
		glyphStyle = m.theme.Alive
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

	var rightPlain, rightStyled string
	rightWidth := 0
	if m.showFork {
		fork := ""
		if r.node.ParentID != "" {
			fork = fmt.Sprintf("%s t%d", g.Fork, r.node.ForkSeq)
		}
		cell := padLeft(fork, forkWidth)
		rightPlain += cell + "  "
		rightStyled += m.theme.Faint.Render(cell) + "  "
		rightWidth += forkWidth + 2
	}
	if m.showProject {
		cell := padRight(truncate(lane.Project, projectWidth), projectWidth)
		rightPlain += cell + "  "
		rightStyled += m.theme.Faint.Render(cell) + "  "
		rightWidth += projectWidth + 2
	}
	msgs := padLeft(fmt.Sprintf("%d", lane.Messages), msgsWidth)
	size := padLeft(humanBytes(lane.Size), sizeWidth)
	age := padLeft(humanAge(m.now().Sub(lane.Updated)), ageWidth)
	status, statusStyle := "idle", m.theme.Faint
	if m.isActive(r.node) {
		status, statusStyle = "active", m.theme.Alive
	}
	status = padLeft(status, statusWidth)
	rightPlain += msgs + "  " + size + "  " + age + "  " + status
	rightStyled += m.theme.Value.Render(msgs) + "  " + m.theme.Faint.Render(size) + "  " +
		m.theme.Dim.Render(age) + "  " + statusStyle.Render(status)
	rightWidth += msgsWidth + 2 + sizeWidth + 2 + ageWidth + 2 + statusWidth

	// row = " " + prefix + glyph + " " + title + suffix + " " + right,
	// so the fixed cost either side of the title is 4 cells.
	titleWidth := m.contentWidth() - 4 - lipgloss.Width(r.prefix) - rightWidth - lipgloss.Width(suffix)
	if titleWidth < 8 {
		titleWidth = 8
	}
	name := padRight(truncate(title, titleWidth), titleWidth)

	rail := ""
	if r.prefix != "" {
		rail = m.theme.Rail.Render(r.prefix)
	}
	plain = " " + r.prefix + g.Lane + " " + name + suffix + " " + rightPlain
	styled = " " + rail + glyphStyle.Render(g.Lane) + " " +
		m.theme.Title.Render(name) + m.theme.Faint.Render(suffix) + " " + rightStyled
	return plain, styled
}

// columns draws the k9s-style header row above the lanes.
func (m Model) columns() string {
	right := ""
	if m.showFork {
		right += padLeft("FORK", forkWidth) + "  "
	}
	if m.showProject {
		right += padRight("PROJECT", projectWidth) + "  "
	}
	right += padLeft("TURNS", msgsWidth) + "  " + padLeft("SIZE", sizeWidth) + "  " +
		padLeft("AGE", ageWidth) + "  " + padLeft("STATUS", statusWidth)

	nameWidth := m.contentWidth() - 2 - lipgloss.Width(right)
	if nameWidth < 8 {
		nameWidth = 8
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
func layout(f *graph.Forest, g Glyphs) []row {
	var out []row
	var walk func(n *graph.Node, ancestors string, last bool, depth int)
	walk = func(n *graph.Node, ancestors string, last bool, depth int) {
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
		for i, c := range n.Children {
			walk(c, childAncestors, i == len(n.Children)-1, depth+1)
		}
	}
	for i, r := range f.Roots {
		walk(r, "", i == len(f.Roots)-1, 0)
	}
	return out
}

// oneLine lives in spineview.go.

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
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
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

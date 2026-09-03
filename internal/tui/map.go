package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Ashes47/braids/internal/core/graph"
)

// activeWindow is how recently a lane must have changed to read as alive.
// Until hooks land, recency is the only liveness braids can honestly claim.
const activeWindow = 10 * time.Minute

// Column widths for the right-hand block. Everything left of it flexes.
const (
	forkWidth    = 8
	projectWidth = 10
	msgsWidth    = 6
	ageWidth     = 7
)

// row is one lane placed on screen together with the tree art leading to it.
type row struct {
	node   *graph.Node
	prefix string
}

// Model is the Map: every conversation and every branch as a single forest.
type Model struct {
	theme Theme
	ascii bool
	now   func() time.Time

	all       []row
	visible   []row
	filter    string
	filtering bool

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
func NewModel(f *graph.Forest, ascii bool) Model {
	m := Model{
		theme:  NewTheme(true, ascii),
		ascii:  ascii,
		now:    time.Now,
		width:  80,
		height: 24,
	}
	m.all = layout(f, m.theme.Glyphs)
	m.visible = m.all
	m.measure()
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
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

// View renders the Map full-screen.
func (m Model) View() tea.View {
	return tea.View{Content: m.render(), AltScreen: true}
}

func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		return m.filterKey(msg)
	}
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
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
	case "/":
		m.filtering = true
	case "esc":
		m.filter = ""
		m.apply()
	}
	m.clamp()
	return m, nil
}

func (m Model) filterKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filtering, m.filter = false, ""
	case "enter":
		m.filtering = false
	case "ctrl+c":
		return m, tea.Quit
	case "backspace":
		if r := []rune(m.filter); len(r) > 0 {
			m.filter = string(r[:len(r)-1])
		}
	default:
		if s := msg.String(); len(s) == 1 {
			m.filter += s
		}
	}
	m.apply()
	m.clamp()
	return m, nil
}

// apply narrows the visible rows to those matching the filter. Filtering drops
// the tree art: a partial forest with dangling connectors reads as corruption.
func (m *Model) apply() {
	if m.filter == "" {
		m.visible = m.all
		return
	}
	needle := strings.ToLower(m.filter)
	m.visible = nil
	for _, r := range m.all {
		l := r.node.Lane
		hay := strings.ToLower(l.Title + " " + l.Project + " " + l.ID)
		if strings.Contains(hay, needle) {
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

// bodyHeight is the terminal height minus the header and footer chrome.
func (m Model) bodyHeight() int {
	h := m.height - 4
	if h < 1 {
		return 1
	}
	return h
}

func (m Model) render() string {
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n")
	b.WriteString(m.columns())
	b.WriteString("\n")

	if len(m.visible) == 0 {
		b.WriteString("  " + m.theme.Empty.Render(m.emptyMessage()) + "\n")
		for i := 1; i < m.bodyHeight(); i++ {
			b.WriteString("\n")
		}
	} else {
		end := min(m.offset+m.bodyHeight(), len(m.visible))
		for i := m.offset; i < end; i++ {
			b.WriteString(m.renderRow(m.visible[i], i == m.cursor))
			b.WriteString("\n")
		}
		for i := end - m.offset; i < m.bodyHeight(); i++ {
			b.WriteString("\n")
		}
	}
	b.WriteString(m.footer())
	return b.String()
}

func (m Model) emptyMessage() string {
	if m.filter != "" {
		return fmt.Sprintf("nothing matches %q", m.filter)
	}
	return "no conversations indexed yet — run: braids index"
}

func (m Model) header() string {
	left := m.theme.Brand.Render("braids") + "  " +
		m.theme.Header.Render(fmt.Sprintf("%d conversations · %d active", len(m.all), m.activeCount()))
	right := ""
	switch {
	case m.filtering:
		right = m.theme.Accent.Render("/" + m.filter + "▏")
	case m.filter != "":
		right = m.theme.Dim.Render("/" + m.filter)
	}
	return " " + spread(left, right, m.width-2)
}

func (m Model) footer() string {
	k := m.theme.Key.Render
	t := m.theme.Footer.Render
	if m.filtering {
		return " " + k("enter") + t(" keep   ") + k("esc") + t(" clear   ") + t("type to filter")
	}
	return " " + k("j/k") + t(" move   ") + k("g/G") + t(" ends   ") +
		k("/") + t(" filter   ") + k("esc") + t(" clear   ") + k("q") + t(" quit")
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
		return m.theme.Selected.Width(m.width).Render(plain)
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
	age := padLeft(humanAge(m.now().Sub(lane.Updated)), ageWidth)
	rightPlain += msgs + "  " + age
	rightStyled += m.theme.Dim.Render(msgs) + "  " + m.theme.Faint.Render(age)
	rightWidth += msgsWidth + 2 + ageWidth

	// row = " " + prefix + glyph + " " + title + suffix + " " + right,
	// so the fixed cost either side of the title is 4 cells.
	titleWidth := m.width - 4 - lipgloss.Width(r.prefix) - rightWidth - lipgloss.Width(suffix)
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
	right += padLeft("TURNS", msgsWidth) + "  " + padLeft("AGE", ageWidth)

	nameWidth := m.width - 2 - lipgloss.Width(right)
	if nameWidth < 8 {
		nameWidth = 8
	}
	return m.theme.Column.Render(" " + padRight("  CONVERSATION", nameWidth) + " " + right)
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

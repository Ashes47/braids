package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/Ashes47/braids/internal/core/index"
	"github.com/Ashes47/braids/internal/core/memory"
	"github.com/Ashes47/braids/internal/core/model"
)

const originSession = "fe00c207-1111-4000-8000-000000000001"

func memoryModel(t *testing.T, load func() ([]memory.Set, error)) Model {
	t.Helper()
	lanes := []index.LaneInfo{
		laneInfo(originSession, "import pipeline", "storefront", 100, time.Hour),
		laneInfo("other000-2222-4000-8000-000000000002", "something else", "storefront", 5, time.Hour),
	}
	if load == nil {
		load = func() ([]memory.Set, error) {
			return []memory.Set{{
				Location: memory.Location{Project: "storefront", Dir: "/m"},
				Memories: []memory.Memory{
					{Name: "shard-manifest", Description: "why twice", Kind: "project",
						Origin: originSession, Modified: now, Links: []string{"reader-contract"}, Listed: true},
					{Name: "alerting-inventory", Description: "what alerts", Kind: "project",
						Origin: originSession, Modified: now, Listed: false},
					{Name: "no-origin", Description: "written by nothing", Kind: "feedback",
						Modified: now, Listed: true},
				},
				Orphaned: []string{"removed-long-ago"},
			}}, nil
		}
	}
	m := NewModel(forestOf(lanes, nil), Options{ASCII: true, Source: "claudecode", LoadMemories: load})
	m.now = func() time.Time { return now }
	m.width, m.height = 100, 24
	return m.openMemories()
}

// The screen groups by project and lands on a memory, never on the heading:
// a cursor on a label has nothing to act on.
func TestMemoryScreenLandsOnAMemory(t *testing.T) {
	m := memoryModel(t, nil)
	if m.mode != memoryMode {
		t.Fatalf("mode = %v, want the memory screen", m.mode)
	}
	entry, ok := m.memoryCursor()
	if !ok {
		t.Fatal("the cursor is not on a memory")
	}
	if entry.Name != "shard-manifest" {
		t.Errorf("landed on %q", entry.Name)
	}
	out := plain(m.renderMemories())
	for _, want := range []string{"storefront · 3", "shard-manifest", "alerting-inventory", "why twice"} {
		if !strings.Contains(out, want) {
			t.Errorf("screen is missing %q:\n%s", want, out)
		}
	}
}

// Moving skips the headings, in both directions and around the ends.
func TestMemoryScreenSkipsHeadings(t *testing.T) {
	m := memoryModel(t, nil)
	for range 6 {
		m = m.memoryKey("j")
		if _, ok := m.memoryCursor(); !ok {
			t.Fatalf("the cursor landed on a heading at row %d", m.memories.cursor)
		}
	}
	for range 6 {
		m = m.memoryKey("k")
		if _, ok := m.memoryCursor(); !ok {
			t.Fatalf("moving up landed on a heading at row %d", m.memories.cursor)
		}
	}
	m = m.memoryKey("G")
	if entry, ok := m.memoryCursor(); !ok || entry.Name != "no-origin" {
		t.Errorf("G landed on %+v, want the last memory", entry)
	}
	m = m.memoryKey("g")
	if entry, ok := m.memoryCursor(); !ok || entry.Name != "shard-manifest" {
		t.Errorf("g landed on %+v, want the first memory", entry)
	}
}

// A memory records the session that wrote it, and that is the edge worth
// having: ↵ goes to the conversation where the decision was made.
func TestMemoryScreenOpensTheConversationThatWroteIt(t *testing.T) {
	m := memoryModel(t, nil)
	m = m.memoryKey("c")
	if m.mode != mapMode || m.memories != nil {
		t.Fatalf("mode = %v, want the map", m.mode)
	}
	if got := m.visible[m.cursor].node.Lane.ID; got != originSession {
		t.Errorf("landed on %s, want the origin conversation", got)
	}
	if !strings.Contains(m.notice, "shard-manifest") {
		t.Errorf("notice = %q, want it to name the memory", m.notice)
	}
}

// Two ways that edge can be absent, each reported rather than silently doing
// nothing.
func TestMemoryScreenSaysWhenItCannotOpenTheConversation(t *testing.T) {
	m := memoryModel(t, nil)
	m = m.memoryKey("G") // no-origin
	m = m.memoryKey("c")
	if m.mode != memoryMode || !m.memories.failed ||
		!strings.Contains(m.memories.notice, "did not record") {
		t.Errorf("notice = %q, want it to say no conversation was recorded", m.memories.notice)
	}

	m = memoryModel(t, func() ([]memory.Set, error) {
		return []memory.Set{{
			Location: memory.Location{Project: "p", Dir: "/m"},
			Memories: []memory.Memory{{Name: "stray", Origin: "deadbeef-0000-4000-8000-000000000009",
				Modified: now, Listed: true}},
		}}, nil
	})
	m = m.memoryKey("c")
	if m.mode != memoryMode || !strings.Contains(m.memories.notice, "not indexed") {
		t.Errorf("notice = %q, want it to say the conversation is not indexed", m.memories.notice)
	}
}

// The index is what a session loads, so an unlisted memory is marked and the
// count is stated: it is the one thing here that is actually broken.
func TestMemoryScreenMarksWhatIsNeverLoaded(t *testing.T) {
	m := memoryModel(t, nil)
	out := plain(m.renderMemories())
	if !strings.Contains(out, "! alerting-inventory") {
		t.Errorf("the unlisted memory is not marked:\n%s", out)
	}
	facts := map[string]string{}
	for _, f := range m.memoryFacts() {
		facts[f.label] = f.value
	}
	if facts["Never loaded"] != "1" {
		t.Errorf("never loaded = %q, want 1", facts["Never loaded"])
	}
	// An index row with no file is a stale pointer, counted apart from a link
	// to a name that does not exist yet.
	if facts["Index stale"] != "1" {
		t.Errorf("index stale = %q, want 1 for the orphaned row", facts["Index stale"])
	}
	if facts["Loose links"] != "1" {
		t.Errorf("loose links = %q, want 1 for the link to reader-contract", facts["Loose links"])
	}
	if facts["Remembered"] != "3 in 1 projects" {
		t.Errorf("remembered = %q", facts["Remembered"])
	}
}

// Without the capability the screen says so rather than opening empty.
func TestMemoryScreenWithoutTheCapability(t *testing.T) {
	lanes := []index.LaneInfo{laneInfo("lane1", "x", "p", 1, time.Hour)}
	m := NewModel(forestOf(lanes, nil), Options{ASCII: true, Source: "claudecode"})
	m.now = func() time.Time { return now }
	m.width, m.height = 90, 20
	m = m.openMemories()
	if m.mode == memoryMode {
		t.Error("the screen opened with no way to read memories")
	}
	if !strings.Contains(m.notice, "no memories") {
		t.Errorf("notice = %q", m.notice)
	}
}

// A failure to read is shown on the screen, not swallowed.
func TestMemoryScreenShowsAReadFailure(t *testing.T) {
	m := memoryModel(t, func() ([]memory.Set, error) {
		return nil, errors.New("permission denied")
	})
	if got := plain(m.renderMemories()); !strings.Contains(got, "permission denied") {
		t.Errorf("screen does not report the failure:\n%s", got)
	}
}

// A legend that teaches a colour the rows do not use teaches nothing.
func TestMemoryMarksWearTheLegendColour(t *testing.T) {
	m := memoryModel(t, func() ([]memory.Set, error) {
		return []memory.Set{{
			Location: memory.Location{Project: "p", Dir: "/m"},
			Memories: []memory.Memory{
				{Name: "fine", Kind: "project", Origin: originSession, Modified: now, Listed: true},
				{Name: "unlisted", Kind: "project", Origin: originSession, Modified: now, Listed: false},
				{Name: "loose", Kind: "project", Origin: originSession, Modified: now,
					Links: []string{"nowhere"}, Listed: true},
			},
		}}, nil
	})
	rows := map[string]string{}
	for _, r := range m.memories.rows {
		if r.memory == nil {
			continue
		}
		rows[r.memory.Name] = m.renderMemoryRow(r, false)
	}
	urgent := m.theme.Urgent.Render(m.theme.Glyphs.Failed)
	accent := m.theme.Accent.Render(m.theme.Glyphs.Agent)
	if !strings.Contains(rows["unlisted"], urgent) {
		t.Errorf("the unlisted mark is not in the legend's colour:\n%q", rows["unlisted"])
	}
	if !strings.Contains(rows["loose"], accent) {
		t.Errorf("the loose-link mark is not in the legend's colour:\n%q", rows["loose"])
	}
	if strings.Contains(rows["fine"], urgent) || strings.Contains(rows["fine"], accent) {
		t.Errorf("a memory with nothing wrong is wearing a mark:\n%q", rows["fine"])
	}
	// And the plain text still lines up: the marks must not widen the column.
	for _, name := range []string{"fine", "unlisted", "loose"} {
		if got := lipgloss.Width(plain(rows[name])); got != m.contentWidth() {
			t.Errorf("row %q is %d columns, want %d", name, got, m.contentWidth())
		}
	}
}

// n and N step between the memories that have something wrong with them.
func TestMemoryNextFlagged(t *testing.T) {
	m := memoryModel(t, nil) // shard-manifest (loose link), alerting-inventory (unlisted), no-origin (fine)
	m = m.memoryKey("n")
	if entry, _ := m.memoryCursor(); entry.Name != "alerting-inventory" {
		t.Errorf("n landed on %q, want the unlisted one", entry.Name)
	}
	m = m.memoryKey("n")
	if entry, _ := m.memoryCursor(); entry.Name != "shard-manifest" {
		t.Errorf("n wrapped to %q, want the one with a loose link", entry.Name)
	}
	m = m.memoryKey("N")
	if entry, _ := m.memoryCursor(); entry.Name != "alerting-inventory" {
		t.Errorf("N landed on %q", entry.Name)
	}

	// Nothing flagged says so rather than sitting still.
	clean := memoryModel(t, func() ([]memory.Set, error) {
		return []memory.Set{{
			Location: memory.Location{Project: "p", Dir: "/m"},
			Memories: []memory.Memory{{Name: "fine", Modified: now, Listed: true}},
		}}, nil
	})
	clean = clean.memoryKey("n")
	if !strings.Contains(clean.memories.notice, "nothing here is unlisted") {
		t.Errorf("notice = %q", clean.memories.notice)
	}
}

// The list says what braids knows about a memory; ↵ shows the memory itself,
// which is the thing you came to check.
func TestMemoryReaderShowsTheText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shard-manifest.md")
	body := "The manifest is written twice.\n\nThe reader needs it before the writer finishes, " +
		"which is a long enough sentence to have to wrap across more than one line of a narrow frame.\n"
	if err := os.WriteFile(path, []byte("---\nname: shard-manifest\ndescription: why twice\n"+
		"metadata:\n  type: project\n---\n\n"+body), 0o600); err != nil {
		t.Fatal(err)
	}
	m := memoryModel(t, func() ([]memory.Set, error) {
		return []memory.Set{{
			Location: memory.Location{Project: "p", Dir: dir},
			Memories: []memory.Memory{{
				Name: "shard-manifest", Description: "why twice", Kind: "project",
				Origin: originSession, Path: path, Modified: now,
				Links: []string{"reader-contract"}, Listed: true,
			}},
		}}, nil
	})

	m = m.memoryKey("enter")
	if m.memories.reading == nil {
		t.Fatal("↵ did not open the memory")
	}
	out := plain(m.renderMemories())
	for _, want := range []string{
		"The manifest is written twice", "shard-manifest", "reader-contract", "why twice",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the reader is missing %q:\n%s", want, out)
		}
	}
	// Frontmatter is not text: it is already in the facts above.
	if strings.Contains(out, "metadata:") || strings.Contains(out, "description: why twice") {
		t.Errorf("frontmatter leaked into the body:\n%s", out)
	}
	// Prose wraps to the frame rather than being cut off.
	for _, line := range strings.Split(out, "\n") {
		if lipgloss.Width(line) > m.width {
			t.Errorf("a line is %d columns wide, frame is %d:\n%q", lipgloss.Width(line), m.width, line)
		}
	}

	// esc returns to the list, not out of the screen.
	m = m.memoryKey("esc")
	if m.memories == nil || m.memories.reading != nil {
		t.Fatal("esc did not return to the list")
	}
	if m.mode != memoryMode {
		t.Error("esc left the memory screen entirely")
	}

	// From the reader, c still reaches the conversation that wrote it.
	m = m.memoryKey("enter")
	m = m.memoryKey("c")
	if m.mode != mapMode {
		t.Errorf("c from the reader went to %v, want the map", m.mode)
	}
}

// Scrolling stops at the ends rather than running off into blank space.
func TestMemoryReaderScrollingStaysInBounds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "long.md")
	var lines []string
	for i := range 60 {
		lines = append(lines, fmt.Sprintf("line %d of the memory", i))
	}
	if err := os.WriteFile(path, []byte("---\nname: long\ndescription: d\nmetadata:\n  type: project\n---\n\n"+
		strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := memoryModel(t, func() ([]memory.Set, error) {
		return []memory.Set{{
			Location: memory.Location{Project: "p", Dir: dir},
			Memories: []memory.Memory{{Name: "long", Path: path, Modified: now, Listed: true}},
		}}, nil
	})
	m = m.memoryKey("enter")

	m = m.memoryKey("k")
	if got := m.memories.reading.offset; got != 0 {
		t.Errorf("scrolled above the first line to %d", got)
	}
	m = m.memoryKey("G")
	last := m.memories.reading.offset
	if last == 0 {
		t.Fatal("G did not scroll")
	}
	m = m.memoryKey("j")
	if got := m.memories.reading.offset; got != last {
		t.Errorf("scrolled past the end to %d, was %d", got, last)
	}
	// The last line is on screen at the bottom, not scrolled out of view.
	if !strings.Contains(plain(m.renderMemories()), "line 59 of the memory") {
		t.Error("the end of the memory is not visible at the bottom")
	}
}

// A memory whose file has gone says so instead of showing an empty frame.
func TestMemoryReaderReportsAMissingFile(t *testing.T) {
	m := memoryModel(t, func() ([]memory.Set, error) {
		return []memory.Set{{
			Location: memory.Location{Project: "p", Dir: "/m"},
			Memories: []memory.Memory{{
				Name: "vanished", Path: filepath.Join("/m", "vanished.md"), Modified: now, Listed: true,
			}},
		}}, nil
	})
	m = m.memoryKey("enter")
	if got := plain(m.renderMemories()); !strings.Contains(got, "vanished.md") {
		t.Errorf("the reader does not say what it could not read:\n%s", got)
	}
}

// curationModel wires the three curation callbacks and records what they were
// asked to do.
type curated struct {
	removed  []string
	renamed  [][2]string
	repaired []string
	fail     error
}

func curationModel(t *testing.T, lanes []index.LaneInfo, log *curated) Model {
	t.Helper()
	sets := func() ([]memory.Set, error) {
		return []memory.Set{{
			Location: memory.Location{Project: "storefront", Dir: "/m"},
			Memories: []memory.Memory{
				{Name: "shard-manifest", Kind: "project", Modified: now, Listed: true},
				{Name: "alerting-inventory", Kind: "project", Modified: now, Listed: false},
			},
			Orphaned: []string{"removed-long-ago"},
		}}, nil
	}
	m := NewModel(forestOf(lanes, nil), Options{
		ASCII: true, Source: "claudecode", LoadMemories: sets,
		RemoveMemory: func(_, name string) error {
			if log.fail != nil {
				return log.fail
			}
			log.removed = append(log.removed, name)
			return nil
		},
		RenameMemory: func(_, from, to string) (int, error) {
			if log.fail != nil {
				return 0, log.fail
			}
			log.renamed = append(log.renamed, [2]string{from, to})
			return 3, nil
		},
		RepairMemoryIndex: func(dir string) (int, int, error) {
			if log.fail != nil {
				return 0, 0, log.fail
			}
			log.repaired = append(log.repaired, dir)
			return 1, 1, nil
		},
	})
	m.now = func() time.Time { return now }
	m.width, m.height = 100, 24
	return m.openMemories()
}

// An idle project can be curated: delete, repair, rename.
func TestMemoryCurationActs(t *testing.T) {
	idle := []index.LaneInfo{laneInfo("a", "finished long ago", "storefront", 10, 72*time.Hour)}

	log := &curated{}
	m := curationModel(t, idle, log)
	m = m.memoryKey("d")
	if len(log.removed) != 1 || log.removed[0] != "shard-manifest" {
		t.Errorf("removed %v", log.removed)
	}
	if !strings.Contains(m.memories.notice, "out of the index") {
		t.Errorf("notice = %q, want it to say the index changed too", m.memories.notice)
	}

	log = &curated{}
	m = curationModel(t, idle, log)
	m = m.memoryKey("i")
	if len(log.repaired) != 1 {
		t.Fatalf("repaired %v", log.repaired)
	}
	if !strings.Contains(m.memories.notice, "nothing loaded") {
		t.Errorf("notice = %q, want it to say what was fixed", m.memories.notice)
	}

	log = &curated{}
	m = curationModel(t, idle, log)
	m = m.memoryKey("r")
	if !m.memories.naming.active {
		t.Fatal("r did not open the rename field")
	}
	if !strings.Contains(plain(m.renderMemories()), "rename to:") {
		t.Error("an open rename field is invisible")
	}
	// The field starts on the current name, so a small correction is small.
	if m.memories.naming.text != "shard-manifest" {
		t.Errorf("field starts as %q", m.memories.naming.text)
	}
	for range len("manifest") {
		m = m.memoryKey("backspace")
	}
	for _, r := range "ordering" {
		m = m.memoryKey(string(r))
	}
	m = m.memoryKey("enter")
	if len(log.renamed) != 1 || log.renamed[0] != [2]string{"shard-manifest", "shard-ordering"} {
		t.Fatalf("renamed %v", log.renamed)
	}
	// A rename that quietly rewrote other memories has to say so.
	if !strings.Contains(m.memories.notice, "3 links followed") {
		t.Errorf("notice = %q, want the links mentioned", m.memories.notice)
	}

	// esc abandons a rename without doing it.
	log = &curated{}
	m = curationModel(t, idle, log)
	m = m.memoryKey("r")
	m = m.memoryKey("esc")
	if m.memories.naming.active || len(log.renamed) != 0 {
		t.Errorf("esc did not abandon the rename: %v", log.renamed)
	}
}

// A project with a session running is refused: braids editing a file a session
// may also be writing is how something a person asked to be remembered gets
// lost.
func TestMemoryCurationRefusesWhileASessionIsLive(t *testing.T) {
	live := []index.LaneInfo{laneInfo("a", "still working", "storefront", 10, time.Minute)}
	live[0].Activity = model.Activity{LastRole: model.RoleUser}

	for _, key := range []string{"d", "i"} {
		log := &curated{}
		m := curationModel(t, live, log)
		if _, running := m.liveIn("storefront"); !running {
			t.Fatalf("the fixture is not live, so %q proves nothing", key)
		}
		m = m.memoryKey(key)
		if len(log.removed) != 0 || len(log.repaired) != 0 {
			t.Errorf("%q acted while a session was live", key)
		}
		if !m.memories.failed || !strings.Contains(m.memories.notice, "still working") {
			t.Errorf("%q: notice = %q, want it to name the conversation", key, m.memories.notice)
		}
	}

	// And a rename typed out is refused at the point of acting, not silently.
	log := &curated{}
	m := curationModel(t, live, log)
	m = m.memoryKey("r")
	// A real change, not the name it already has: renaming to the same name is
	// a no-op and says nothing, which would prove nothing here.
	for range len("manifest") {
		m = m.memoryKey("backspace")
	}
	for _, r := range "ordering" {
		m = m.memoryKey(string(r))
	}
	m = m.memoryKey("enter")
	if len(log.renamed) != 0 {
		t.Error("a rename went through while a session was live")
	}
	if !m.memories.failed || !strings.Contains(m.memories.notice, "still working") {
		t.Errorf("the refused rename said %q", m.memories.notice)
	}

	// A memory under a project with nothing running is still editable.
	quiet := []index.LaneInfo{laneInfo("b", "elsewhere", "other-project", 10, time.Minute)}
	quiet[0].Activity = model.Activity{LastRole: model.RoleUser}
	log = &curated{}
	m = curationModel(t, quiet, log)
	m = m.memoryKey("d")
	if len(log.removed) != 1 {
		t.Errorf("a busy project elsewhere blocked an edit: %v", log.removed)
	}
	if m.memories.failed {
		t.Errorf("a permitted edit was reported as a failure: %q", m.memories.notice)
	}
}

// A failure is reported rather than leaving the screen looking as if it worked.
func TestMemoryCurationReportsFailure(t *testing.T) {
	idle := []index.LaneInfo{laneInfo("a", "finished", "storefront", 10, 72*time.Hour)}
	log := &curated{fail: errors.New("read-only file system")}
	m := curationModel(t, idle, log)
	m = m.memoryKey("d")
	if !m.memories.failed || !strings.Contains(m.memories.notice, "read-only") {
		t.Errorf("notice = %q (failed=%v)", m.memories.notice, m.memories.failed)
	}
}

// The header is sized from headerContent and drawn from headerContent. When
// the reader drew its own facts while the plan was measured from the list, a
// long memory name overflowed the row and the first key binding vanished.
func TestHeaderIsSizedFromTheScreenItDraws(t *testing.T) {
	long := strings.Repeat("release-", 4) + "hashes" // longer than any list value
	m := memoryModel(t, func() ([]memory.Set, error) {
		return []memory.Set{{
			Location: memory.Location{Project: "Mailer", Dir: "/m"},
			Memories: []memory.Memory{{
				Name: long, Kind: "project", Modified: now, Origin: originSession,
				Links: []string{"one-link", "another-link"}, Listed: true, Path: "/nope.md",
			}},
		}}, nil
	})
	m = m.memoryKey("enter")

	lines := strings.Split(plain(m.memoryInfo()), "\n")
	if len(lines) < 4 {
		t.Fatalf("the reader header is only %d rows", len(lines))
	}
	// Every key the reader offers is listed. A key that works and is not shown
	// may as well not exist.
	header := strings.Join(lines, "\n")
	for _, h := range readingHints() {
		if !strings.Contains(header, h.action) {
			t.Errorf("the header does not list %q:\n%s", h.action, header)
		}
	}
	// And the rows line up: one short row means one that overflowed.
	widths := map[int]int{}
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			widths[lipgloss.Width(line)]++
		}
	}
	if len(widths) != 1 {
		t.Errorf("header rows have differing widths %v:\n%s", widths, header)
	}
	for width := range widths {
		if width > m.width {
			t.Errorf("header rows are %d columns, frame is %d", width, m.width)
		}
	}
}

// Repair on a memory whose only mark is a loose link must say why it left it
// alone. "The index already agrees with the files" is true and unhelpful while
// the row under the cursor is visibly marked.
func TestRepairExplainsALooseLink(t *testing.T) {
	idle := []index.LaneInfo{laneInfo("a", "finished", "Mailer", 10, 72*time.Hour)}
	sets := func() ([]memory.Set, error) {
		return []memory.Set{{
			Location: memory.Location{Project: "Mailer", Dir: "/m"},
			Memories: []memory.Memory{{
				Name: "payments-live", Kind: "project", Modified: now, Listed: true,
				Links: []string{"mailer-billing-model"},
			}},
		}}, nil
	}
	m := NewModel(forestOf(idle, nil), Options{
		ASCII: true, Source: "claudecode", LoadMemories: sets,
		RepairMemoryIndex: func(string) (int, int, error) { return 0, 0, nil },
	})
	m.now = func() time.Time { return now }
	m.width, m.height = 110, 24
	m = m.openMemories()

	// The row is marked, and the mark is the link one, not the index one.
	entry, ok := m.memoryCursor()
	if !ok {
		t.Fatal("no memory under the cursor")
	}
	row := m.memories.shown[m.memories.cursor]
	if !m.marked(row) || !entry.Listed {
		t.Fatalf("the fixture is not the case under test: marked=%v listed=%v", m.marked(row), entry.Listed)
	}

	m = m.repairMemoryIndex()
	notice := m.memories.notice
	for _, want := range []string{
		"already agrees", "payments-live", "[[mailer-billing-model]]", "note rather than a fault",
	} {
		if !strings.Contains(notice, want) {
			t.Errorf("notice %q is missing %q", notice, want)
		}
	}
	if m.memories.failed {
		t.Error("a correct index was reported as a failure")
	}

	// The legend calls it what it is, not "missing".
	var meanings []string
	for _, g := range m.memoryGlyphs() {
		meanings = append(meanings, g.meaning)
	}
	joined := strings.Join(meanings, " | ")
	if strings.Contains(joined, "missing") {
		t.Errorf("the legend still calls a loose link missing: %s", joined)
	}
	if !strings.Contains(joined, "not written yet") {
		t.Errorf("the legend does not say what the mark means: %s", joined)
	}
}

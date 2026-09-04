package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/Ashes47/braids/internal/core/index"
	"github.com/Ashes47/braids/internal/core/memory"
)

const originSession = "fe00c207-1111-4000-8000-000000000001"

func memoryModel(t *testing.T, load func() ([]memory.Set, error)) Model {
	t.Helper()
	lanes := []index.LaneInfo{
		laneInfo(originSession, "annotation pipeline", "microagi", 100, time.Hour),
		laneInfo("other000-2222-4000-8000-000000000002", "something else", "microagi", 5, time.Hour),
	}
	if load == nil {
		load = func() ([]memory.Set, error) {
			return []memory.Set{{
				Location: memory.Location{Project: "microagi", Dir: "/m"},
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
	for _, want := range []string{"microagi · 3", "shard-manifest", "alerting-inventory", "why twice"} {
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
	m = m.memoryKey("enter")
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
	m = m.memoryKey("enter")
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
	m = m.memoryKey("enter")
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

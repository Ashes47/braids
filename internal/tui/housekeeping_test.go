package tui

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Ashes47/braids/internal/core/hooks"
	"github.com/Ashes47/braids/internal/core/index"
	"github.com/Ashes47/braids/internal/core/model"
	"github.com/Ashes47/braids/internal/core/trash"
)

type housekeeping struct {
	archived map[string]bool
	deleted  []string
	restored []string
	purged   []string
	entries  []trash.Entry
	failDel  error
}

func keepingModel(t *testing.T, lanes []index.LaneInfo) (Model, *housekeeping) {
	t.Helper()
	h := &housekeeping{archived: map[string]bool{}}
	m := NewModel(forestOf(lanes, nil), Options{
		ASCII:    true,
		Archived: h.archived,
		Archive: func(id string, on bool) error {
			if on {
				h.archived[id] = true
			} else {
				delete(h.archived, id)
			}
			return nil
		},
		Delete: func(id string) (int64, error) {
			if h.failDel != nil {
				return 0, h.failDel
			}
			h.deleted = append(h.deleted, id)
			return 2048, nil
		},
		LoadBin: func() ([]trash.Entry, error) { return h.entries, nil },
		Restore: func(id string) error { h.restored = append(h.restored, id); return nil },
		Purge:   func(id string) error { h.purged = append(h.purged, id); return nil },
	})
	m.now = func() time.Time { return now }
	m.width, m.height = 100, 20
	return m, h
}

func press(m Model, key string) Model {
	updated, _ := m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
	return updated.(Model)
}

func TestArchiveHidesWithoutDeleting(t *testing.T) {
	lanes := []index.LaneInfo{
		laneInfo("a", "still working on this", "app", 5, time.Hour),
		laneInfo("b", "finished last month", "app", 5, 30*24*time.Hour),
	}
	m, h := keepingModel(t, lanes)
	m.cursor = 1

	m = press(m, "a")
	if !h.archived["b"] {
		t.Fatal("a should archive the selected conversation")
	}
	if len(m.visible) != 1 || m.visible[0].node.Lane.ID != "a" {
		t.Errorf("an archived conversation should leave the map, got %d lanes", len(m.visible))
	}
	out := plain(m.render())
	if !strings.Contains(out, "archived") {
		t.Errorf("expected confirmation:\n%s", out)
	}
	if !strings.Contains(out, "· 1 archived hidden") {
		t.Errorf("a map that hides things must say so:\n%s", out)
	}

	// A shows them again, marked as set aside.
	m = press(m, "A")
	if len(m.visible) != 2 {
		t.Fatalf("A should reveal archived conversations, got %d", len(m.visible))
	}
	if out := plain(m.render()); !strings.Contains(out, "showing 1 archived") {
		t.Errorf("expected the revealed state to be named:\n%s", out)
	}

	// And a brings it back for good.
	for i, r := range m.visible {
		if r.node.Lane.ID == "b" {
			m.cursor = i
		}
	}
	if press(m, "a"); h.archived["b"] {
		t.Error("a on an archived conversation should unarchive it")
	}
}

func TestDeleteIsReportedAndUndoable(t *testing.T) {
	lanes := []index.LaneInfo{laneInfo("a", "old scratch", "app", 3, 40*24*time.Hour)}
	lanes[0].Activity = model.Activity{LastRole: model.RoleAssistant}
	m, h := keepingModel(t, lanes)

	m = press(m, "d")
	if len(h.deleted) != 1 || h.deleted[0] != "a" {
		t.Fatalf("deleted = %v", h.deleted)
	}
	out := plain(m.render())
	for _, want := range []string{"deleted a", "2 kB reclaimed", "u to recover", "children unaffected"} {
		if !strings.Contains(out, want) {
			t.Errorf("delete notice missing %q:\n%s", want, out)
		}
	}

}

func TestDeleteRefusesARunningConversation(t *testing.T) {
	lanes := []index.LaneInfo{laneInfo("a", "mid tool call", "app", 5, time.Second)}
	lanes[0].Activity = model.Activity{LastRole: model.RoleAssistant, LastWasToolCall: true}
	m, h := keepingModel(t, lanes)

	m = press(m, "d")
	if len(h.deleted) != 0 {
		t.Fatal("deleting a running conversation must be refused")
	}
	if !strings.Contains(plain(m.render()), "still running") {
		t.Error("expected the reason")
	}
}

func TestDeleteFailureIsReported(t *testing.T) {
	lanes := []index.LaneInfo{laneInfo("a", "scratch", "app", 3, time.Hour)}
	lanes[0].Activity = model.Activity{LastRole: model.RoleAssistant}
	m, h := keepingModel(t, lanes)
	h.failDel = errors.New("permission denied")

	if !strings.Contains(plain(press(m, "d").render()), "permission denied") {
		t.Error("a failed delete must be reported")
	}
}

func TestDeleteFromTheSpineIsRedirected(t *testing.T) {
	lanes := []index.LaneInfo{laneInfo("a", "scratch", "app", 3, time.Hour)}
	m, h := keepingModel(t, lanes)
	m.mode = spineMode
	m.spine = &spineState{lane: lanes[0]}

	if out := plain(press(m, "d").renderSpine()); !strings.Contains(out, "delete from the map") {
		t.Errorf("deleting from inside a conversation should be redirected:\n%s", out)
	}
	if len(h.deleted) != 0 {
		t.Fatal("nothing should have been deleted")
	}
}

func TestArchivingKeepsTheTree(t *testing.T) {
	lanes := []index.LaneInfo{
		laneInfo("root", "the original", "app", 20, time.Hour),
		laneInfo("kid", "a branch", "app", 5, time.Hour),
		laneInfo("grandkid", "a branch of the branch", "app", 3, time.Hour),
	}
	m, _ := keepingModel(t, lanes)
	m = m.adopt(forestOf(lanes, map[string]string{"kid": "root", "grandkid": "kid"}))

	connectors := func(m Model) []string {
		out := make([]string, 0, len(m.visible))
		for _, r := range m.visible {
			out = append(out, r.prefix)
		}
		return out
	}
	if got := connectors(m); got[0] != "" || got[1] == "" || got[2] == "" {
		t.Fatalf("the unfiltered map should be a tree, got prefixes %q", got)
	}

	// Archiving the middle conversation must not flatten the map, and must not
	// take its branch away with it.
	for i, r := range m.visible {
		if r.node.Lane.ID == "kid" {
			m.cursor = i
		}
	}
	m = press(m, "a")
	if len(m.visible) != 2 {
		t.Fatalf("want root and grandkid, got %d rows", len(m.visible))
	}
	if m.visible[1].node.Lane.ID != "grandkid" {
		t.Errorf("a branch of an archived conversation should remain, got %q", m.visible[1].node.Lane.ID)
	}
	if m.visible[1].prefix == "" {
		t.Error("it should still be drawn under the conversation that survives")
	}
	if !strings.Contains(plain(m.render()), "1 archived hidden") {
		t.Error("the title should say something is held back")
	}
}

func binEntries(now time.Time) []trash.Entry {
	return []trash.Entry{
		{ID: "e3", Label: "deleted an hour ago", At: now.Add(-time.Hour), Bytes: 4096},
		{ID: "e2", Label: "the one you want back", At: now.Add(-48 * time.Hour), Bytes: 2048},
		{ID: "e1", Label: "nearly expired", At: now.Add(-(trash.Retention - time.Hour)), Bytes: 1024},
	}
}

func TestTheBinLetsYouRecoverSomethingDeletedDaysAgo(t *testing.T) {
	lanes := []index.LaneInfo{laneInfo("a", "still here", "app", 5, time.Hour)}
	m, h := keepingModel(t, lanes)
	h.entries = binEntries(now)

	m = press(m, "u")
	if m.mode != binMode {
		t.Fatal("u should open the bin")
	}
	out := plain(m.renderBin())
	for _, want := range []string{
		"Deleted:", "Holding:", "Kept for:", "14 days",
		"Deleted(all)[3]", "the one you want back", "2d ago", "restore",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the bin is missing %q:\n%s", want, out)
		}
	}

	// Pick the eighth-of-ten problem: move down and bring it back.
	m = m.binKey("j")
	m = m.binKey("enter")
	if len(h.restored) != 1 || h.restored[0] != "e2" {
		t.Fatalf("restored = %v, want the selected entry", h.restored)
	}
	if len(m.bin.entries) != 2 {
		t.Errorf("a restored entry should leave the bin, %d remain", len(m.bin.entries))
	}
	if !strings.Contains(plain(m.renderBin()), "back on the map") {
		t.Error("expected confirmation")
	}
}

func TestTheBinShowsHowLongIsLeft(t *testing.T) {
	m, h := keepingModel(t, []index.LaneInfo{laneInfo("a", "x", "app", 1, time.Hour)})
	h.entries = binEntries(now)
	m = press(m, "u")

	out := plain(m.renderBin())
	if !strings.Contains(out, "in 13d") {
		t.Errorf("a fresh deletion should show its full retention:\n%s", out)
	}
	if !strings.Contains(out, "in 1h") {
		t.Errorf("one about to go should say so:\n%s", out)
	}
	// The one nearest expiry is warned about in the facts.
	if !strings.Contains(out, "Next to go") {
		t.Error("the facts should name the deadline")
	}
}

func TestPurgingFromTheBinIsFinal(t *testing.T) {
	m, h := keepingModel(t, []index.LaneInfo{laneInfo("a", "x", "app", 1, time.Hour)})
	h.entries = binEntries(now)
	m = press(m, "u")

	m = m.binKey("d")
	if len(h.purged) != 1 || h.purged[0] != "e3" {
		t.Fatalf("purged = %v", h.purged)
	}
	if !strings.Contains(plain(m.renderBin()), "gone for good") {
		t.Error("expected the finality to be stated")
	}
}

func TestAnEmptyBinSaysSo(t *testing.T) {
	m, _ := keepingModel(t, []index.LaneInfo{laneInfo("a", "x", "app", 1, time.Hour)})
	m = press(m, "u")
	if !strings.Contains(plain(m.renderBin()), "nothing has been deleted") {
		t.Error("expected an empty-state message")
	}
	m = m.binKey("esc")
	if m.mode != mapMode {
		t.Error("esc should return to the map")
	}
}

func TestWorkProductsAreShownAndCanGoSeparately(t *testing.T) {
	heavy := laneInfo("a", "long debugging session", "app", 900, time.Hour)
	heavy.Size = 145 << 20
	heavy.ArtifactBytes = 1970 << 20
	heavy.ArtifactPath = "/tmp/jobs/a"
	heavy.Activity = model.Activity{LastRole: model.RoleAssistant}
	light := laneInfo("b", "a quick question", "app", 4, time.Hour)
	light.Size = 40 << 10

	var discarded []string
	m, _ := keepingModel(t, []index.LaneInfo{heavy, light})
	m.deleteWorkFn = func(id string) (int64, error) {
		discarded = append(discarded, id)
		return 1970 << 20, nil
	}
	m.width = 128

	out := plain(m.render())
	if !strings.Contains(out, "WORK") {
		t.Fatalf("a conversation with work products should earn the column:\n%s", out)
	}
	heavyRow := rowFor(t, out, "long debugging session")
	if !strings.Contains(heavyRow, "1.9 GB") || !strings.Contains(heavyRow, "145 MB") {
		t.Errorf("the row should carry both sizes: %q", heavyRow)
	}
	// A conversation with none leaves the cell empty rather than printing zero.
	if lightRow := rowFor(t, out, "a quick question"); strings.Contains(lightRow, "0 B") {
		t.Errorf("an empty work cell should stay empty: %q", lightRow)
	}

	m = press(m, "D")
	if len(discarded) != 1 || discarded[0] != "a" {
		t.Fatalf("D discarded %v", discarded)
	}
	notice := plain(m.render())
	if !strings.Contains(notice, "1.9 GB of work products") {
		t.Errorf("expected the amount reclaimed:\n%s", notice)
	}
	if !strings.Contains(notice, "conversation is untouched") {
		t.Error("the notice must say the conversation survives — that is the whole point")
	}
}

func TestNoWorkColumnWithoutWorkProducts(t *testing.T) {
	m, _ := keepingModel(t, []index.LaneInfo{laneInfo("a", "just a chat", "app", 4, time.Hour)})
	m.width = 128
	if strings.Contains(plain(m.render()), "WORK") {
		t.Error("the column should not appear when nothing has work products")
	}
}

func TestRenameGivesAConversationYourOwnName(t *testing.T) {
	var renamed [][2]string
	lanes := []index.LaneInfo{laneInfo("a", "Debug import pipeline dataset issue", "app", 5, time.Hour)}
	m, _ := keepingModel(t, lanes)
	m.renameFn = func(id, name string) error {
		renamed = append(renamed, [2]string{id, name})
		return nil
	}

	m = press(m, "r")
	if !m.naming.active {
		t.Fatal("r should open the name field")
	}
	// Pre-filled with what it is called now, so a tweak is a tweak.
	if m.naming.text != "Debug import pipeline dataset issue" {
		t.Errorf("field = %q, want the current name", m.naming.text)
	}
	if out := plain(m.render()); !strings.Contains(out, "name:") || !strings.Contains(out, "esc cancel") {
		t.Errorf("expected an inline field under the row:\n%s", out)
	}

	m = m.renameKey("esc")
	if m.naming.active {
		t.Fatal("esc should close the field")
	}
	if len(renamed) != 0 {
		t.Error("cancelling must not rename")
	}

	m = press(m, "r")
	m.naming.text = "blobstore density"
	m = m.renameKey("enter")
	if len(renamed) != 1 || renamed[0][1] != "blobstore density" {
		t.Fatalf("renamed = %v", renamed)
	}
	if !strings.Contains(plain(m.render()), "renamed to blobstore density") {
		t.Error("expected confirmation")
	}
}

func TestClearingANameRestoresTheOriginal(t *testing.T) {
	var renamed [][2]string
	m, _ := keepingModel(t, []index.LaneInfo{laneInfo("a", "my own name", "app", 5, time.Hour)})
	m.renameFn = func(id, name string) error {
		renamed = append(renamed, [2]string{id, name})
		return nil
	}
	m = press(m, "r")
	m.naming.text = "   "
	m = m.renameKey("enter")

	if len(renamed) != 1 || renamed[0][1] != "" {
		t.Fatalf("an emptied field should clear the name, got %v", renamed)
	}
	if !strings.Contains(plain(m.render()), "back to what the harness called it") {
		t.Error("expected the effect to be stated")
	}
}

func TestArchivingKeepsTheCursorNearWhereItWas(t *testing.T) {
	lanes := []index.LaneInfo{
		laneInfo("a", "first", "app", 5, time.Hour),
		laneInfo("b", "second", "app", 5, time.Hour),
		laneInfo("c", "third", "app", 5, time.Hour),
		laneInfo("d", "fourth", "app", 5, time.Hour),
	}
	m, _ := keepingModel(t, lanes)
	m.cursor = 2 // on "third"

	m = press(m, "a")
	if len(m.visible) != 3 {
		t.Fatalf("archiving should hide it, %d rows remain", len(m.visible))
	}
	// The row above, not the top of the list: tidying should not cost your place.
	if got := m.visible[m.cursor].node.Lane.ID; got != "b" {
		t.Errorf("cursor landed on %q, want the row above the archived one", got)
	}
}

func TestArchivingTheFirstRowStaysAtTheTop(t *testing.T) {
	lanes := []index.LaneInfo{
		laneInfo("a", "first", "app", 5, time.Hour),
		laneInfo("b", "second", "app", 5, time.Hour),
	}
	m, _ := keepingModel(t, lanes)
	m.cursor = 0

	m = press(m, "a")
	if m.cursor != 0 || m.visible[0].node.Lane.ID != "b" {
		t.Errorf("archiving the first row should leave the cursor at the top, got %d", m.cursor)
	}
}

func TestArchivingTheLastRowStaysInRange(t *testing.T) {
	lanes := []index.LaneInfo{
		laneInfo("a", "first", "app", 5, time.Hour),
		laneInfo("b", "second", "app", 5, time.Hour),
	}
	m, _ := keepingModel(t, lanes)
	m.cursor = 1

	m = press(m, "a")
	if m.cursor != 0 || len(m.visible) != 1 {
		t.Errorf("cursor = %d with %d rows, want 0 of 1", m.cursor, len(m.visible))
	}
}

func TestArchivingTheOnlyRowLeavesAnEmptyMap(t *testing.T) {
	m, _ := keepingModel(t, []index.LaneInfo{laneInfo("a", "only", "app", 5, time.Hour)})
	m = press(m, "a")
	if len(m.visible) != 0 || m.cursor != 0 {
		t.Errorf("cursor = %d with %d rows", m.cursor, len(m.visible))
	}
	if !strings.Contains(plain(m.render()), "1 archived hidden") {
		t.Error("the title should still explain where everything went")
	}
}

func TestNeedsYouIsTheLoudestThingOnScreen(t *testing.T) {
	blocked := laneInfo("a", "stuck on a permission", "app", 5, 10*time.Second)
	blocked.Activity = model.Activity{LastRole: model.RoleAssistant, LastWasToolCall: true}
	owed := laneInfo("b", "answered a while ago", "app", 5, time.Hour)
	owed.Activity = model.Activity{LastRole: model.RoleAssistant}

	m, _ := keepingModel(t, []index.LaneInfo{blocked, owed})
	m.liveFn = func() (map[string]hooks.Event, error) {
		return map[string]hooks.Event{
			"a": {Name: hooks.PermissionRequest, At: blocked.Updated},
		}, nil
	}
	m.refreshLive()
	m.width = 128

	out := plain(m.render())
	if !strings.Contains(out, "needs you") {
		t.Fatalf("a reported block should be named:\n%s", out)
	}
	// A different shape, not only a different colour: in a long list shape
	// carries further than hue.
	stuck := rowFor(t, out, "stuck on a permission")
	if !strings.Contains(stuck, m.theme.Glyphs.Needs) {
		t.Errorf("the blocked row should carry its own mark: %q", stuck)
	}
	if quiet := rowFor(t, out, "answered a while ago"); strings.Contains(quiet, m.theme.Glyphs.Needs) {
		t.Errorf("a conversation merely owed a reply is not urgent: %q", quiet)
	}

	// And the loud style is spent on nothing else.
	urgent := m.theme.Urgent.Render("x")
	for _, g := range m.mapGlyphs() {
		if g.meaning != "stopped, needs you" && g.style.Render("x") == urgent {
			t.Errorf("%q shares the urgent style", g.meaning)
		}
	}
}

func TestAnArchivedRowReadsAsSetAside(t *testing.T) {
	lanes := []index.LaneInfo{
		laneInfo("a", "put away", "app", 5, time.Hour),
		laneInfo("b", "still active", "app", 5, time.Hour),
	}
	lanes[0].Activity = model.Activity{LastRole: model.RoleAssistant}
	lanes[1].Activity = model.Activity{LastRole: model.RoleAssistant}
	m, _ := keepingModel(t, lanes)
	m.width = 128

	m = press(m, "a") // archive the first
	m = press(m, "A") // and reveal it

	out := plain(m.render())
	away := rowFor(t, out, "put away")
	// Named as archived rather than by what it was doing when it was put away,
	// which is no longer the useful thing about it.
	if !strings.Contains(away, "archived") {
		t.Errorf("an archived row should say so: %q", away)
	}
	if !strings.Contains(away, m.theme.Glyphs.Archived) {
		t.Errorf("and carry its own mark: %q", away)
	}
	if active := rowFor(t, out, "still active"); strings.Contains(active, "archived") {
		t.Errorf("a live row must not: %q", active)
	}
}

// styleOf pulls the escape sequence a row draws its name with.
func styleOf(t *testing.T, m Model, laneID string) string {
	t.Helper()
	for _, r := range m.visible {
		if r.node.Lane.ID != laneID {
			continue
		}
		_, styled := m.rowParts(r)
		codes := regexp.MustCompile(`\x1b\[[0-9;]*m`).FindAllString(styled, -1)
		if len(codes) < 3 {
			t.Fatalf("row %s has too few styled parts", laneID)
		}
		return codes[2] // gutter, glyph, then the name
	}
	t.Fatalf("no row for %s", laneID)
	return ""
}

func TestRowEmphasis(t *testing.T) {
	blocked := laneInfo("a", "blocked", "app", 5, 5*time.Second)
	blocked.Activity = model.Activity{LastRole: model.RoleAssistant, LastWasToolCall: true}
	away := laneInfo("b", "put away", "app", 5, time.Hour)
	away.Activity = model.Activity{LastRole: model.RoleAssistant}
	plain := laneInfo("c", "ordinary", "app", 5, time.Hour)
	plain.Activity = model.Activity{LastRole: model.RoleAssistant}

	m, _ := keepingModel(t, []index.LaneInfo{blocked, away, plain})
	m.archived = map[string]bool{"b": true}
	m.liveFn = func() (map[string]hooks.Event, error) {
		return map[string]hooks.Event{"a": {Name: hooks.PermissionRequest, At: blocked.Updated}}, nil
	}
	m.refreshLive()
	m.showArchived = true
	m.apply()

	urgent, dull, ordinary := styleOf(t, m, "a"), styleOf(t, m, "b"), styleOf(t, m, "c")
	if urgent == ordinary {
		t.Error("a blocked conversation should not be drawn like an ordinary one")
	}
	if dull == ordinary {
		t.Error("an archived conversation should be duller than an ordinary one")
	}
	// Colour and weight, not a filled block: a background reads as a box drawn
	// round the row and is heavier than anything else on the screen.
	if strings.Contains(urgent, "48;2;") {
		t.Errorf("the urgent style paints a background: %q", urgent)
	}
	if !strings.Contains(urgent, "1;") {
		t.Errorf("the urgent style is not bold: %q", urgent)
	}
}

// Every list screen filters the same way, with f, and says in its title that
// rows are being held back.
func TestEveryListScreenFilters(t *testing.T) {
	t.Run("bin", func(t *testing.T) {
		lanes := []index.LaneInfo{laneInfo("a", "still here", "app", 5, time.Hour)}
		m, h := keepingModel(t, lanes)
		h.entries = []trash.Entry{
			{ID: "1", Label: "blobstore density", At: now, Bytes: 10},
			{ID: "2", Label: "worktree probe", At: now, Bytes: 20},
		}
		m = press(m, "u")
		m = m.binKey("f")
		for _, r := range "worktree" {
			m = m.binKey(string(r))
		}
		out := plain(m.renderBin())
		if !strings.Contains(out, "worktree probe") || strings.Contains(out, "blobstore density") {
			t.Errorf("filtered bin:\n%s", out)
		}
		if !strings.Contains(out, "/worktree") {
			t.Errorf("the title does not say what is filtered:\n%s", out)
		}
		// A filter that matches nothing says so rather than looking empty.
		for range len("worktree") {
			m = m.binKey("backspace")
		}
		for _, r := range "zzz" {
			m = m.binKey(string(r))
		}
		if got := plain(m.renderBin()); !strings.Contains(got, `nothing deleted matches "zzz"`) {
			t.Errorf("empty filtered bin:\n%s", got)
		}
	})

	t.Run("work", func(t *testing.T) {
		m, _ := workModel(t, nil)
		m, _ = m.workKey("enter") // into tmp: nodes.json, deep
		m, _ = m.workKey("f")
		for _, r := range "nodes" {
			m, _ = m.workKey(string(r))
		}
		out := plain(m.renderWork())
		if !strings.Contains(out, "nodes.json") || strings.Contains(out, "deep/") {
			t.Errorf("filtered work products:\n%s", out)
		}
		if !strings.Contains(out, "/nodes") {
			t.Errorf("the title does not say what is filtered:\n%s", out)
		}
	})

	t.Run("memories", func(t *testing.T) {
		m := memoryModel(t, nil)
		m = m.memoryKey("f")
		for _, r := range "alert" {
			m = m.memoryKey(string(r))
		}
		out := plain(m.renderMemories())
		if !strings.Contains(out, "alerting-inventory") || strings.Contains(out, "shard-manifest") {
			t.Errorf("filtered memories:\n%s", out)
		}
		// The cursor never rests on a project heading, filtered or not.
		if _, ok := m.memoryCursor(); !ok {
			t.Error("the cursor landed on a heading after filtering")
		}
		// esc peels the filter before leaving the screen.
		m = m.memoryKey("esc")
		if m.mode != memoryMode {
			t.Error("esc left the screen instead of clearing the filter")
		}
	})
}

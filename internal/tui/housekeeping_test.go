package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

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
		"Deleted[3]", "the one you want back", "2d ago", "restore",
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

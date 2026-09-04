package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Ashes47/braids/internal/core/index"
	"github.com/Ashes47/braids/internal/core/model"
)

type housekeeping struct {
	archived map[string]bool
	deleted  []string
	undone   int
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
		Undo: func() (int64, error) { h.undone++; return 2048, nil },
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
	for _, want := range []string{"deleted a", "2 kB reclaimed", "u to undo", "children unaffected"} {
		if !strings.Contains(out, want) {
			t.Errorf("delete notice missing %q:\n%s", want, out)
		}
	}

	m = press(m, "u")
	if h.undone != 1 {
		t.Error("u should undo the deletion")
	}
	if !strings.Contains(plain(m.render()), "restored") {
		t.Error("expected confirmation of the restore")
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

package claudecode

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Ashes47/braids/internal/core/model"
	"github.com/Ashes47/braids/internal/core/store"
)

// mergeFixture is a conversation and a branch of it that has diverged. The
// branch copies the shared records verbatim, IDs included, as a fork does.
func mergeFixture(t *testing.T) (*Source, model.Lane, model.Lane) {
	t.Helper()
	shared := []string{
		`{"type":"user","uuid":"u1","parentUuid":null,"sessionId":"base","message":{"role":"user","content":"say root"}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"base","message":{"role":"assistant","content":[{"type":"text","text":"ROOT"}]}}`,
	}
	root := writeLanes(t, "-p", map[string][]string{
		"base.jsonl": append(append([]string{}, shared...),
			`{"type":"user","uuid":"u2","parentUuid":"a1","sessionId":"base","message":{"role":"user","content":"say trunk"}}`,
			`{"type":"assistant","uuid":"a2","parentUuid":"u2","sessionId":"base","message":{"role":"assistant","content":[{"type":"text","text":"TRUNK"}]}}`,
		),
		"side.jsonl": append(append([]string{}, shared...),
			`{"type":"user","uuid":"s1","parentUuid":"a1","sessionId":"side","message":{"role":"user","content":"say leaf"}}`,
			`{"type":"assistant","uuid":"s2","parentUuid":"s1","sessionId":"side","message":{"role":"assistant","content":[{"type":"text","text":"LEAF"}]}}`,
		),
	})
	s := New(root)
	lanes, err := s.Lanes(context.Background())
	if err != nil {
		t.Fatalf("Lanes: %v", err)
	}
	var base, side model.Lane
	for _, l := range lanes {
		if l.ID == "base" {
			base = l
		} else {
			side = l
		}
	}
	return s, base, side
}

func TestPlanMergeReportsWhatWouldBeCarriedOver(t *testing.T) {
	s, base, side := mergeFixture(t)
	plan, err := s.PlanMerge(context.Background(), store.MergeRequest{Base: base, Incoming: side})
	if err != nil {
		t.Fatalf("PlanMerge: %v", err)
	}
	if plan.Shared != 2 {
		t.Errorf("Shared = %d, want the two records a fork copied", plan.Shared)
	}
	if plan.IncomingTurns != 2 || plan.BaseTurns != 4 {
		t.Errorf("turns = %d incoming / %d base, want 2 and 4", plan.IncomingTurns, plan.BaseTurns)
	}
}

func TestMergeJoinsBothHistoriesAndLeavesTheOriginalsAlone(t *testing.T) {
	s, base, side := mergeFixture(t)
	before := map[string][32]byte{}
	for _, l := range []model.Lane{base, side} {
		body, err := os.ReadFile(l.Path)
		if err != nil {
			t.Fatalf("read %s: %v", l.ID, err)
		}
		before[l.ID] = sha256.Sum256(body)
	}

	merged, err := s.Merge(context.Background(), store.MergeRequest{Base: base, Incoming: side, Name: "joined"})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	for _, l := range []model.Lane{base, side} {
		body, err := os.ReadFile(l.Path)
		if err != nil {
			t.Fatalf("re-read %s: %v", l.ID, err)
		}
		if sha256.Sum256(body) != before[l.ID] {
			t.Errorf("merging modified %s; a merge must only ever add a conversation", l.ID)
		}
	}

	lanes, err := s.Lanes(context.Background())
	if err != nil {
		t.Fatalf("Lanes: %v", err)
	}
	for _, l := range lanes {
		if l.ID != merged.ID {
			continue
		}
		got := collect(t, s, l)
		want := []string{"say root", "ROOT", "say trunk", "TRUNK", "say leaf", "LEAF"}
		if len(got) != len(want) {
			t.Fatalf("merged conversation has %d turns, want %d", len(got), len(want))
		}
		for i, w := range want {
			if got[i].Text() != w {
				t.Errorf("turn %d = %q, want %q", i, got[i].Text(), w)
			}
		}
		// The carried-over turns must chain onto the end of the base, or the
		// harness would reconstruct only part of it.
		if got[4].ParentID != got[3].ID {
			t.Errorf("the branch was not grafted onto the base's leaf")
		}
	}
}

func TestMergeGivesCarriedRecordsNewIdentities(t *testing.T) {
	s, base, side := mergeFixture(t)
	merged, err := s.Merge(context.Background(), store.MergeRequest{Base: base, Incoming: side, Name: "joined"})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	body, err := os.ReadFile(merged.Path)
	if err != nil {
		t.Fatalf("read merged: %v", err)
	}
	// Reusing the branch's IDs would leave a lane that reads as a fork of the
	// thing it was merged from.
	for _, id := range []string{`"s1"`, `"s2"`} {
		if strings.Contains(string(body), id) {
			t.Errorf("carried-over record kept its old identity %s", id)
		}
	}
	for _, l := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		var fields map[string]any
		if err := json.Unmarshal([]byte(l), &fields); err != nil {
			t.Fatalf("merged line is not JSON: %s", l)
		}
		if v, ok := fields["sessionId"]; ok && v != merged.ID {
			t.Errorf("sessionId = %v, want %s", v, merged.ID)
		}
	}
}

func TestMergeRefusesWhatCannotBeMerged(t *testing.T) {
	s, base, side := mergeFixture(t)
	tests := []struct {
		name string
		req  store.MergeRequest
		want string
	}{
		{"into itself", store.MergeRequest{Base: base, Incoming: base}, "into itself"},
		{"nothing chosen", store.MergeRequest{Base: base}, "both conversations"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.Merge(context.Background(), tt.req); err == nil ||
				!strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}

	// A branch that has not diverged carries nothing over.
	if _, err := s.Merge(context.Background(), store.MergeRequest{Base: side, Incoming: base}); err != nil {
		t.Fatalf("merging the other way should work: %v", err)
	}
	stale := store.MergeRequest{Base: base, Incoming: model.Lane{ID: "copy", Path: base.Path}}
	if _, err := s.Merge(context.Background(), stale); err == nil ||
		!strings.Contains(err.Error(), "does not already have") {
		t.Errorf("merging a conversation into itself by another name should be refused, got %v", err)
	}
}

func TestMergeKeepsItsOwnName(t *testing.T) {
	// The base carries a title of its own. Copying it would land after the
	// merged conversation's title and win, because a reader takes the last.
	root := writeLanes(t, "-p", map[string][]string{
		"base.jsonl": {
			`{"type":"custom-title","customTitle":"the original","sessionId":"base"}`,
			`{"type":"user","uuid":"u1","parentUuid":null,"sessionId":"base","message":{"role":"user","content":"one"}}`,
			// The base carried on after the branch left, or there would be
			// nothing to merge.
			`{"type":"user","uuid":"b1","parentUuid":"u1","sessionId":"base","message":{"role":"user","content":"base carried on"}}`,
			`{"type":"last-prompt","leafUuid":"b1","sessionId":"base"}`,
		},
		"side.jsonl": {
			`{"type":"custom-title","customTitle":"the branch","sessionId":"side"}`,
			`{"type":"user","uuid":"u1","parentUuid":null,"sessionId":"side","message":{"role":"user","content":"one"}}`,
			`{"type":"user","uuid":"s1","parentUuid":"u1","sessionId":"side","message":{"role":"user","content":"two"}}`,
		},
	})
	s := New(root)
	lanes, err := s.Lanes(context.Background())
	if err != nil {
		t.Fatalf("Lanes: %v", err)
	}
	var base, side model.Lane
	for _, l := range lanes {
		if l.ID == "base" {
			base = l
		} else {
			side = l
		}
	}

	merged, err := s.Merge(context.Background(), store.MergeRequest{
		Base: base, Incoming: side, Name: "the original + the branch",
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	enriched, err := s.Enrich(context.Background(), merged)
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if enriched.Title != "the original + the branch" {
		t.Errorf("merged conversation is called %q — a copied title overwrote its own", enriched.Title)
	}

	body, err := os.ReadFile(merged.Path)
	if err != nil {
		t.Fatalf("read merged: %v", err)
	}
	if n := strings.Count(string(body), `"type":"custom-title"`); n != 1 {
		t.Errorf("merged transcript holds %d titles, want exactly its own", n)
	}
	if n := strings.Count(string(body), `"type":"last-prompt"`); n != 1 {
		t.Errorf("merged transcript holds %d leaf markers, want exactly its own", n)
	}
}

func TestMergeRefusesWhenTheBranchAlreadyContainsTheBase(t *testing.T) {
	// A branch cut from a conversation that then never carried on is a strict
	// superset of it. Merging them would write a copy of the branch.
	root := writeLanes(t, "-p", map[string][]string{
		"base.jsonl": {
			`{"type":"user","uuid":"u1","parentUuid":null,"sessionId":"base","message":{"role":"user","content":"one"}}`,
			`{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"base","message":{"role":"assistant","content":[{"type":"text","text":"ONE"}]}}`,
		},
		"side.jsonl": {
			`{"type":"user","uuid":"u1","parentUuid":null,"sessionId":"side","message":{"role":"user","content":"one"}}`,
			`{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"side","message":{"role":"assistant","content":[{"type":"text","text":"ONE"}]}}`,
			`{"type":"user","uuid":"s1","parentUuid":"a1","sessionId":"side","message":{"role":"user","content":"two"}}`,
		},
	})
	s := New(root)
	lanes, err := s.Lanes(context.Background())
	if err != nil {
		t.Fatalf("Lanes: %v", err)
	}
	var base, side model.Lane
	for _, l := range lanes {
		if l.ID == "base" {
			base = l
		} else {
			side = l
		}
	}
	side.Title = "the branch"

	plan, err := s.PlanMerge(context.Background(), store.MergeRequest{Base: base, Incoming: side})
	if err != nil {
		t.Fatalf("PlanMerge: %v", err)
	}
	if plan.BaseOnlyTurns != 0 {
		t.Errorf("BaseOnlyTurns = %d, want 0 — this conversation added nothing after the branch left",
			plan.BaseOnlyTurns)
	}
	if plan.Worthwhile() {
		t.Error("a merge that would only copy the branch is not worthwhile")
	}

	_, err = s.Merge(context.Background(), store.MergeRequest{Base: base, Incoming: side})
	if err == nil {
		t.Fatal("want a refusal")
	}
	if !strings.Contains(err.Error(), "already contains everything") ||
		!strings.Contains(err.Error(), "the branch") {
		t.Errorf("err = %v, want it to name the branch and say why", err)
	}
}

func TestAMergeWorthMakingIsAllowed(t *testing.T) {
	s, base, side := mergeFixture(t)
	plan, err := s.PlanMerge(context.Background(), store.MergeRequest{Base: base, Incoming: side})
	if err != nil {
		t.Fatalf("PlanMerge: %v", err)
	}
	// Both sides carried on after they parted, so neither contains the other.
	if plan.BaseOnlyTurns != 2 || plan.IncomingTurns != 2 {
		t.Errorf("plan = %+v, want two turns unique to each side", plan)
	}
	if !plan.Worthwhile() {
		t.Error("this is exactly the case a merge exists for")
	}
}

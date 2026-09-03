package claudecode

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ashes47/braids/internal/core/model"
	"github.com/Ashes47/braids/internal/core/store"
)

// branchFixture is a lane with bookkeeping records threaded through the chain,
// the way Claude Code actually writes one.
func branchFixture(t *testing.T) (*Source, model.Lane, string) {
	t.Helper()
	root := writeLanes(t, "-p", map[string][]string{"src.jsonl": {
		`{"type":"ai-title","aiTitle":"original","sessionId":"src"}`,
		`{"type":"user","uuid":"u1","parentUuid":null,"sessionId":"src","message":{"role":"user","content":"first"}}`,
		`{"type":"attachment","uuid":"att1","parentUuid":"u1","sessionId":"src"}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"att1","sessionId":"src","message":{"role":"assistant","content":[{"type":"text","text":"answer one"}]}}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1","session_id":"src","message":{"role":"user","content":"second"}}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2","sessionId":"src","message":{"role":"assistant","content":[{"type":"text","text":"answer two"}]}}`,
	}})
	s := New(root)
	lanes, err := s.Lanes(context.Background())
	if err != nil {
		t.Fatalf("Lanes: %v", err)
	}
	return s, lanes[0], filepath.Join(root, "-p")
}

func hashFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(sha256.New().Sum(b))
}

func TestBranchWritesAPrefixAndLeavesTheSourceAlone(t *testing.T) {
	s, lane, _ := branchFixture(t)
	before := hashFile(t, lane.Path)

	branch, err := s.Branch(context.Background(), store.BranchRequest{
		Lane: lane, AtMessage: "a1", Name: "try another way",
	})
	if err != nil {
		t.Fatalf("Branch: %v", err)
	}

	if after := hashFile(t, lane.Path); after != before {
		t.Fatal("the source transcript was modified; a branch must only ever add a file")
	}
	if branch.ID == lane.ID || branch.Path == lane.Path {
		t.Fatalf("branch reused the source identity: %+v", branch)
	}
	if branch.Title != "try another way" {
		t.Errorf("title = %q", branch.Title)
	}

	// Reading the branch back must yield exactly the prefix, chain intact.
	lanes, err := s.Lanes(context.Background())
	if err != nil {
		t.Fatalf("Lanes: %v", err)
	}
	var got []model.Message
	for _, l := range lanes {
		if l.ID != branch.ID {
			continue
		}
		title, err := s.Title(context.Background(), l)
		if err != nil || title != "try another way" {
			t.Errorf("branch title on reload = %q (%v)", title, err)
		}
		got = collect(t, s, l)
	}
	if len(got) != 2 {
		t.Fatalf("branch has %d messages, want 2 (u1, a1)", len(got))
	}
	if got[0].Text() != "first" || got[1].Text() != "answer one" {
		t.Errorf("branch content = %q, %q", got[0].Text(), got[1].Text())
	}
	if got[1].ParentID != "u1" {
		t.Errorf("chain broken across the copied attachment: parent = %q", got[1].ParentID)
	}
}

func TestBranchRewritesEverySessionID(t *testing.T) {
	s, lane, _ := branchFixture(t)
	branch, err := s.Branch(context.Background(), store.BranchRequest{
		Lane: lane, AtMessage: "u2", Name: "n",
	})
	if err != nil {
		t.Fatalf("Branch: %v", err)
	}
	body, err := os.ReadFile(branch.Path)
	if err != nil {
		t.Fatalf("read branch: %v", err)
	}
	if strings.Contains(string(body), `"src"`) {
		t.Errorf("source session id leaked into the branch:\n%s", body)
	}
	// Both spellings Claude Code uses must be rewritten.
	for _, l := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		var fields map[string]any
		if err := json.Unmarshal([]byte(l), &fields); err != nil {
			t.Fatalf("branch line is not JSON: %s", l)
		}
		for _, key := range []string{"sessionId", "session_id"} {
			if v, ok := fields[key]; ok && v != branch.ID {
				t.Errorf("%s = %v, want %s", key, v, branch.ID)
			}
		}
	}
}

func TestBranchPinsTheLeaf(t *testing.T) {
	s, lane, _ := branchFixture(t)
	branch, err := s.Branch(context.Background(), store.BranchRequest{Lane: lane, AtMessage: "a1"})
	if err != nil {
		t.Fatalf("Branch: %v", err)
	}
	body, err := os.ReadFile(branch.Path)
	if err != nil {
		t.Fatalf("read branch: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	var last map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		t.Fatalf("last line: %v", err)
	}
	if last["type"] != "last-prompt" || last["leafUuid"] != "a1" {
		t.Errorf("last record = %v, want a last-prompt pinned to a1", last)
	}
	// Nothing after the branch point may survive.
	if strings.Contains(string(body), "answer two") || strings.Contains(string(body), `"u2"`) {
		t.Errorf("content after the branch point leaked in:\n%s", body)
	}
}

func TestBranchRefusesBadRequests(t *testing.T) {
	s, lane, dir := branchFixture(t)
	countLanes := func() int {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read dir: %v", err)
		}
		return len(entries)
	}
	before := countLanes()

	tests := []struct {
		name, at, want string
	}{
		{"no turn chosen", "", "no turn chosen"},
		{"unknown turn", "nope", "not in this conversation"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.Branch(context.Background(), store.BranchRequest{Lane: lane, AtMessage: tt.at})
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}
	if after := countLanes(); after != before {
		t.Errorf("a refused branch left %d files behind", after-before)
	}
}

func TestBranchAtTheRootKeepsOneTurn(t *testing.T) {
	s, lane, _ := branchFixture(t)
	branch, err := s.Branch(context.Background(), store.BranchRequest{Lane: lane, AtMessage: "u1"})
	if err != nil {
		t.Fatalf("Branch: %v", err)
	}
	lanes, err := s.Lanes(context.Background())
	if err != nil {
		t.Fatalf("Lanes: %v", err)
	}
	for _, l := range lanes {
		if l.ID != branch.ID {
			continue
		}
		if got := collect(t, s, l); len(got) != 1 || got[0].Text() != "first" {
			t.Errorf("branch at the root = %+v, want just the first turn", got)
		}
	}
}

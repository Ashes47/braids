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
)

// laneWithAgent writes a lane plus one subagent transcript beneath it.
func laneWithAgent(t *testing.T) (*Source, model.Subagent, string) {
	t.Helper()
	root := writeLanes(t, "-p", map[string][]string{"parent.jsonl": {
		`{"type":"user","uuid":"u1","parentUuid":null,"sessionId":"parent","message":{"role":"user","content":"go look"}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"parent","message":{"role":"assistant",` +
			`"content":[{"type":"tool_use","id":"toolu_1","name":"Agent","input":{}}]}}`,
	}})
	dir := filepath.Join(root, "-p", "parent", "subagents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	agentPath := filepath.Join(dir, "agent-abc.jsonl")
	body := strings.Join([]string{
		`{"type":"user","uuid":"s1","parentUuid":null,"isSidechain":true,"agentId":"agent-abc","sessionId":"parent","message":{"role":"user","content":"count the files"}}`,
		`{"type":"assistant","uuid":"s2","parentUuid":"s1","isSidechain":true,"agentId":"agent-abc","sessionId":"parent","message":{"role":"assistant","content":[{"type":"text","text":"there are four"}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(agentPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write agent: %v", err)
	}
	meta := `{"agentType":"Explore","description":"Count files","toolUseId":"toolu_1","spawnDepth":1}`
	if err := os.WriteFile(filepath.Join(dir, "agent-abc.meta.json"), []byte(meta), 0o600); err != nil {
		t.Fatalf("write meta: %v", err)
	}

	s := New(root)
	lanes, err := s.Lanes(context.Background())
	if err != nil {
		t.Fatalf("Lanes: %v", err)
	}
	agents, err := s.Subagents(context.Background(), lanes[0])
	if err != nil {
		t.Fatalf("Subagents: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("want 1 subagent, got %d", len(agents))
	}
	return s, agents[0], agentPath
}

func TestSubagentsAreDiscoveredWithTheirMeta(t *testing.T) {
	_, agent, _ := laneWithAgent(t)
	if agent.Type != "Explore" || agent.Task != "Count files" {
		t.Errorf("meta not read: %+v", agent)
	}
	if agent.ToolUseID != "toolu_1" {
		t.Errorf("ToolUseID = %q — without it the agent cannot be placed", agent.ToolUseID)
	}
	if agent.Depth != 1 || agent.Messages != 2 {
		t.Errorf("depth/messages = %d/%d, want 1/2", agent.Depth, agent.Messages)
	}
}

func TestPromoteMakesASubagentAConversation(t *testing.T) {
	s, agent, agentPath := laneWithAgent(t)
	before, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatalf("read agent: %v", err)
	}

	lane, err := s.Promote(context.Background(), agent)
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	after, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatalf("re-read agent: %v", err)
	}
	if sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("the subagent's own transcript was modified")
	}

	// It must land beside the parent conversation, not inside the sidechain
	// directory, or it would not be listed as a lane at all.
	projectDir := filepath.Clean(filepath.Join(agentPath, "..", "..", ".."))
	if filepath.Dir(lane.Path) != projectDir {
		t.Errorf("promoted lane landed at %s, want it in %s", lane.Path, projectDir)
	}
	if lane.Title != "Explore: Count files" {
		t.Errorf("title = %q", lane.Title)
	}

	body, err := os.ReadFile(lane.Path)
	if err != nil {
		t.Fatalf("read promoted: %v", err)
	}
	for _, l := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		var fields map[string]any
		if err := json.Unmarshal([]byte(l), &fields); err != nil {
			t.Fatalf("promoted line is not JSON: %s", l)
		}
		if v, ok := fields["isSidechain"]; ok && v != false {
			t.Errorf("isSidechain survived: %s", l)
		}
		if _, ok := fields["agentId"]; ok {
			t.Errorf("agentId survived: %s", l)
		}
		if v, ok := fields["sessionId"]; ok && v != lane.ID {
			t.Errorf("sessionId = %v, want %s", v, lane.ID)
		}
	}

	// And it reads back as an ordinary conversation.
	lanes, err := s.Lanes(context.Background())
	if err != nil {
		t.Fatalf("Lanes: %v", err)
	}
	for _, l := range lanes {
		if l.ID != lane.ID {
			continue
		}
		got := collect(t, s, l)
		if len(got) != 2 || got[0].Text() != "count the files" || got[1].Text() != "there are four" {
			t.Errorf("promoted conversation = %+v", got)
		}
	}
}

func TestPromoteRefusesAnEmptyPath(t *testing.T) {
	s, _, _ := laneWithAgent(t)
	if _, err := s.Promote(context.Background(), model.Subagent{}); err == nil {
		t.Fatal("want an error for a subagent with no transcript")
	}
}

func TestSubagentsOfALaneWithNone(t *testing.T) {
	root := writeLanes(t, "-p", map[string][]string{"solo.jsonl": {
		`{"type":"user","uuid":"u1","parentUuid":null,"message":{"role":"user","content":"hi"}}`,
	}})
	s := New(root)
	lanes, err := s.Lanes(context.Background())
	if err != nil {
		t.Fatalf("Lanes: %v", err)
	}
	agents, err := s.Subagents(context.Background(), lanes[0])
	if err != nil || agents != nil {
		t.Errorf("a lane that spawned nothing should report none, got %v, %v", agents, err)
	}
}

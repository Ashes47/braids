package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ashes47/braids/internal/core/model"
)

// writeLanes builds a fake ~/.claude/projects tree and returns its root.
func writeLanes(t *testing.T, project string, files map[string][]string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for name, lines := range files {
		body := strings.Join(lines, "\n") + "\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return root
}

func laneByID(t *testing.T, lanes []model.Lane, id string) model.Lane {
	t.Helper()
	for _, l := range lanes {
		if l.ID == id {
			return l
		}
	}
	t.Fatalf("lane %q not found in %d lanes", id, len(lanes))
	return model.Lane{}
}

func collect(t *testing.T, s *Source, lane model.Lane) []model.Message {
	t.Helper()
	var got []model.Message
	err := s.Messages(context.Background(), lane, func(m model.Message) error {
		got = append(got, m)
		return nil
	})
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	return got
}

func TestLanesDiscoversTranscriptsAndTitles(t *testing.T) {
	root := writeLanes(t, "-Users-me-src-app", map[string][]string{
		"a.jsonl": {
			`{"type":"ai-title","aiTitle":"model wrote this","sessionId":"a"}`,
			`{"type":"custom-title","customTitle":"user wrote this","sessionId":"a"}`,
			`{"type":"user","uuid":"u1","parentUuid":null,"message":{"role":"user","content":"hi"}}`,
		},
		"b.jsonl": {
			`{"type":"agent-name","agentName":"fallback name","sessionId":"b"}`,
		},
	})
	// A subagent transcript lives one level deeper and must not become a lane.
	sub := filepath.Join(root, "-Users-me-src-app", "a", "subagents")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "agent-x.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write sub: %v", err)
	}

	lanes, err := New(root).Lanes(context.Background())
	if err != nil {
		t.Fatalf("Lanes: %v", err)
	}
	if len(lanes) != 2 {
		t.Fatalf("want 2 lanes (subagent excluded), got %d", len(lanes))
	}
	a := laneByID(t, lanes, "a")
	if a.Title != "user wrote this" {
		t.Errorf("custom title must win: got %q", a.Title)
	}
	if a.Project != "app" {
		t.Errorf("project = %q, want %q", a.Project, "app")
	}
	if b := laneByID(t, lanes, "b"); b.Title != "fallback name" {
		t.Errorf("agent name fallback: got %q", b.Title)
	}
}

func TestMessagesSkipsBookkeepingRecords(t *testing.T) {
	root := writeLanes(t, "-p", map[string][]string{"s.jsonl": {
		`{"type":"attachment","uuid":"att","parentUuid":null}`,
		`{"type":"ai-title","aiTitle":"t","sessionId":"s"}`,
		`{"type":"user","uuid":"u1","parentUuid":null,"isMeta":true,"message":{"role":"user","content":"meta"}}`,
		`{"type":"user","uuid":"u2","parentUuid":"att","message":{"role":"user","content":"real question"}}`,
		`not json at all`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u2","message":{"role":"assistant","content":[{"type":"text","text":"real answer"}]}}`,
	}})
	s := New(root)
	lanes, err := s.Lanes(context.Background())
	if err != nil {
		t.Fatalf("Lanes: %v", err)
	}
	got := collect(t, s, lanes[0])
	if len(got) != 2 {
		t.Fatalf("want 2 conversational messages, got %d", len(got))
	}
	if got[0].Text() != "real question" || got[0].Role != model.RoleUser {
		t.Errorf("first message = %+v", got[0])
	}
	if got[1].Text() != "real answer" || got[1].Role != model.RoleAssistant {
		t.Errorf("second message = %+v", got[1])
	}
}

func TestParentIDStitchesCompactionBoundary(t *testing.T) {
	root := writeLanes(t, "-p", map[string][]string{"s.jsonl": {
		`{"type":"user","uuid":"before","parentUuid":null,"message":{"role":"user","content":"early work"}}`,
		`{"type":"system","uuid":"boundary","parentUuid":null,"logicalParentUuid":"before","subtype":"compact_boundary"}`,
		`{"type":"user","uuid":"after","parentUuid":null,"logicalParentUuid":"boundary","isCompactSummary":true,` +
			`"message":{"role":"user","content":"summary of the earlier conversation"}}`,
	}})
	s := New(root)
	lanes, err := s.Lanes(context.Background())
	if err != nil {
		t.Fatalf("Lanes: %v", err)
	}
	got := collect(t, s, lanes[0])
	if len(got) != 2 {
		t.Fatalf("want 2 messages, got %d", len(got))
	}
	if got[0].ParentID != "" {
		t.Errorf("true root must have no parent, got %q", got[0].ParentID)
	}
	// Without the logicalParentUuid fallback this is "", which would split one
	// conversation into one lane per compaction.
	if got[1].ParentID != "boundary" {
		t.Errorf("compaction not stitched: ParentID = %q, want %q", got[1].ParentID, "boundary")
	}
}

func TestParsePartsHandlesEveryContentShape(t *testing.T) {
	root := writeLanes(t, "-p", map[string][]string{"s.jsonl": {
		`{"type":"assistant","uuid":"a1","parentUuid":null,"message":{"role":"assistant","content":[` +
			`{"type":"thinking","thinking":"let me look"},` +
			`{"type":"tool_use","name":"Bash","input":{"command":"echo hi"}},` +
			`{"type":"text","text":"done"}]}}`,
		`{"type":"user","uuid":"u1","parentUuid":"a1","message":{"role":"user","content":[` +
			`{"type":"tool_result","content":[{"type":"text","text":"hi"}]}]}}`,
		`{"type":"user","uuid":"u2","parentUuid":"u1","message":{"role":"user","content":[` +
			`{"type":"tool_result","content":"plain string result"}]}}`,
	}})
	s := New(root)
	lanes, err := s.Lanes(context.Background())
	if err != nil {
		t.Fatalf("Lanes: %v", err)
	}
	got := collect(t, s, lanes[0])
	if len(got) != 3 {
		t.Fatalf("want 3 messages, got %d", len(got))
	}

	kinds := make([]model.PartKind, 0, 3)
	for _, p := range got[0].Parts {
		kinds = append(kinds, p.Kind)
	}
	want := []model.PartKind{model.PartThinking, model.PartToolUse, model.PartText}
	if len(kinds) != len(want) {
		t.Fatalf("parts = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("parts = %v, want %v", kinds, want)
		}
	}
	if tool := got[0].Parts[1]; tool.Tool != "Bash" || !strings.Contains(tool.Text, "echo hi") {
		t.Errorf("tool_use = %+v", tool)
	}
	if got[0].Text(model.PartText) != "done" {
		t.Errorf("text-only filter = %q", got[0].Text(model.PartText))
	}
	if got[1].Text() != "hi" {
		t.Errorf("block tool_result = %q", got[1].Text())
	}
	if got[2].Text() != "plain string result" {
		t.Errorf("string tool_result = %q", got[2].Text())
	}
}

func TestMessagesPropagatesVisitError(t *testing.T) {
	root := writeLanes(t, "-p", map[string][]string{"s.jsonl": {
		`{"type":"user","uuid":"u1","parentUuid":null,"message":{"role":"user","content":"one"}}`,
		`{"type":"user","uuid":"u2","parentUuid":"u1","message":{"role":"user","content":"two"}}`,
	}})
	s := New(root)
	lanes, err := s.Lanes(context.Background())
	if err != nil {
		t.Fatalf("Lanes: %v", err)
	}
	sentinel := context.Canceled
	seen := 0
	err = s.Messages(context.Background(), lanes[0], func(model.Message) error {
		seen++
		return sentinel
	})
	if err != sentinel {
		t.Errorf("err = %v, want %v", err, sentinel)
	}
	if seen != 1 {
		t.Errorf("visited %d messages, want 1 before stopping", seen)
	}
}

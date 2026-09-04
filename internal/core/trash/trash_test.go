package trash

import (
	"os"
	"path/filepath"
	"testing"
)

// conversation writes a transcript plus the directory beside it.
func conversation(t *testing.T, dir, id string) (string, string) {
	t.Helper()
	transcript := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(transcript, []byte("{\"type\":\"user\"}\n"), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	side := filepath.Join(dir, id, "subagents")
	if err := os.MkdirAll(side, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(side, "agent-1.jsonl"), []byte("{}\n{}\n"), 0o600); err != nil {
		t.Fatalf("write agent: %v", err)
	}
	return transcript, filepath.Join(dir, id)
}

func TestDiscardAndRestore(t *testing.T) {
	project := t.TempDir()
	transcript, side := conversation(t, project, "abc")
	bin := New(filepath.Join(t.TempDir(), "trash"))

	entry, err := bin.Discard([]string{transcript, side})
	if err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if len(entry.Items) != 2 {
		t.Fatalf("moved %d paths, want the transcript and its directory", len(entry.Items))
	}
	if entry.Bytes == 0 {
		t.Error("reclaimed size should count what was moved, directories included")
	}
	for _, p := range []string{transcript, side} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s survived the deletion", p)
		}
	}

	if err := bin.Restore(entry); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	for _, p := range []string{transcript, filepath.Join(side, "subagents", "agent-1.jsonl")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was not restored: %v", p, err)
		}
	}
}

func TestDiscardSkipsWhatIsNotThere(t *testing.T) {
	project := t.TempDir()
	transcript, _ := conversation(t, project, "abc")
	bin := New(filepath.Join(t.TempDir(), "trash"))

	// A conversation with no subagent directory: the caller offers both paths
	// without having to check which exist.
	entry, err := bin.Discard([]string{transcript, filepath.Join(project, "nothing-here")})
	if err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if len(entry.Items) != 1 {
		t.Errorf("moved %d paths, want just the transcript", len(entry.Items))
	}
}

func TestDiscardNothingLeavesNoTrace(t *testing.T) {
	binDir := filepath.Join(t.TempDir(), "trash")
	bin := New(binDir)
	entry, err := bin.Discard([]string{filepath.Join(t.TempDir(), "absent")})
	if err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if len(entry.Items) != 0 {
		t.Fatal("nothing existed to move")
	}
	if entries, err := os.ReadDir(binDir); err == nil && len(entries) != 0 {
		t.Errorf("an empty deletion left %d directories behind", len(entries))
	}
}

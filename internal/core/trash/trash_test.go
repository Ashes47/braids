package trash

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

	entry, err := bin.Discard("a conversation", []string{transcript, side})
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
	entry, err := bin.Discard("a conversation", []string{transcript, filepath.Join(project, "nothing-here")})
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
	entry, err := bin.Discard("a conversation", []string{filepath.Join(t.TempDir(), "absent")})
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

func TestTheBinSurvivesRestart(t *testing.T) {
	project := t.TempDir()
	transcript, side := conversation(t, project, "abc")
	dir := filepath.Join(t.TempDir(), "trash")

	// Deleted by one session...
	entry, err := New(dir).Discard("nvidia delivery", []string{transcript, side})
	if err != nil {
		t.Fatalf("Discard: %v", err)
	}

	// ...and recovered by another, days later, with nothing held in memory.
	later := New(dir)
	listed, err := later.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("the bin listed %d entries, want 1", len(listed))
	}
	if listed[0].Label != "nvidia delivery" || listed[0].Bytes != entry.Bytes {
		t.Errorf("entry lost its details: %+v", listed[0])
	}
	if _, err := later.RestoreByID(listed[0].ID); err != nil {
		t.Fatalf("RestoreByID: %v", err)
	}
	if _, err := os.Stat(transcript); err != nil {
		t.Errorf("restore did not put the transcript back: %v", err)
	}
	if left, _ := later.List(); len(left) != 0 {
		t.Errorf("a restored entry should leave the bin, %d remain", len(left))
	}
}

func TestListIsNewestFirstAndExpiryIsReported(t *testing.T) {
	project := t.TempDir()
	dir := filepath.Join(t.TempDir(), "trash")
	bin := New(dir)
	for _, id := range []string{"one", "two"} {
		transcript, _ := conversation(t, project, id)
		if _, err := bin.Discard(id, []string{transcript}); err != nil {
			t.Fatalf("Discard %s: %v", id, err)
		}
	}
	entries, err := bin.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 || entries[0].Label != "two" {
		t.Fatalf("want the most recent first, got %+v", entries)
	}
	if got := entries[0].Expires().Sub(entries[0].At); got != Retention {
		t.Errorf("expiry = %v after deletion, want %v", got, Retention)
	}
}

func TestExpireRemovesOnlyWhatIsPastRetention(t *testing.T) {
	project := t.TempDir()
	bin := New(filepath.Join(t.TempDir(), "trash"))
	transcript, _ := conversation(t, project, "abc")
	if _, err := bin.Discard("recent", []string{transcript}); err != nil {
		t.Fatalf("Discard: %v", err)
	}

	gone, _, err := bin.Expire(time.Now())
	if err != nil || gone != 0 {
		t.Fatalf("a fresh deletion must not expire: %d, %v", gone, err)
	}
	gone, bytes, err := bin.Expire(time.Now().Add(Retention + time.Hour))
	if err != nil {
		t.Fatalf("Expire: %v", err)
	}
	if gone != 1 || bytes == 0 {
		t.Errorf("expired %d entries holding %d bytes, want 1 and non-zero", gone, bytes)
	}
	if entries, _ := bin.List(); len(entries) != 0 {
		t.Errorf("the bin should be empty, %d remain", len(entries))
	}
}

func TestPurgeIsFinal(t *testing.T) {
	project := t.TempDir()
	bin := New(filepath.Join(t.TempDir(), "trash"))
	transcript, _ := conversation(t, project, "abc")
	entry, err := bin.Discard("scratch", []string{transcript})
	if err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if err := bin.Purge(entry.ID); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if entries, _ := bin.List(); len(entries) != 0 {
		t.Error("a purged entry should be gone")
	}
	if _, err := bin.RestoreByID(entry.ID); err == nil {
		t.Error("a purged entry must not be recoverable")
	}
}

// Purge deletes a whole tree, and filepath.Join resolves ".." straight out of
// the bin. The guard is at the destructive call, so a caller cannot reach past
// it however the ID was obtained.
func TestBinRefusesAnIDThatIsNotOneOfItsOwn(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "keep")
	if err := os.MkdirAll(filepath.Join(outside, "work"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	bin := New(filepath.Join(root, "bin"))

	for _, id := range []string{"..", "../keep", "../..", "", ".", "a/b", "/etc"} {
		if err := bin.Purge(id); err == nil {
			t.Errorf("Purge(%q) was allowed", id)
		}
		if _, err := bin.RestoreByID(id); err == nil {
			t.Errorf("RestoreByID(%q) was allowed", id)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "work")); err != nil {
		t.Errorf("a directory outside the bin was removed: %v", err)
	}
}

// Several files sharing a basename, discarded together — what selecting scratch
// files from different directories does. Naming destinations by basename alone
// silently overwrote one with the other and lost it.
func TestDiscardKeepsFilesThatShareAName(t *testing.T) {
	root := t.TempDir()
	want := map[string]string{}
	var paths []string
	for _, sub := range []string{"a", "b", "a/deeper"} {
		dir := filepath.Join(root, sub)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "data.json")
		body := "contents of " + sub
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		want[path] = body
		paths = append(paths, path)
	}

	bin := New(filepath.Join(root, "bin"))
	entry, err := bin.Discard("three files, one name", paths)
	if err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if len(entry.Items) != len(paths) {
		t.Fatalf("recorded %d items, want %d", len(entry.Items), len(paths))
	}
	// Each has its own place in the bin, or one of them is already gone.
	seen := map[string]bool{}
	for _, item := range entry.Items {
		if seen[item.To] {
			t.Fatalf("two files share the destination %s", item.To)
		}
		seen[item.To] = true
	}
	for path := range want {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("%s was not moved", path)
		}
	}

	if err := bin.Restore(entry); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	for path, body := range want {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("lost %s: %v", path, err)
			continue
		}
		if string(got) != body {
			t.Errorf("%s came back as %q, want %q", path, got, body)
		}
	}
}

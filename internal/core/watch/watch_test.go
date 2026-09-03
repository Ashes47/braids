package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// waitForChange reports whether a signal arrives within a generous window.
func waitForChange(t *testing.T, w *Watcher) bool {
	t.Helper()
	select {
	case <-w.Changes():
		return true
	case <-time.After(3 * time.Second):
		return false
	}
}

func newWatcher(t *testing.T) (*Watcher, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "-a-project"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	w, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w, root
}

func TestWatcherReportsANewTranscript(t *testing.T) {
	w, root := newWatcher(t)
	path := filepath.Join(root, "-a-project", "new.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !waitForChange(t, w) {
		t.Fatal("a new transcript should be reported")
	}
}

func TestWatcherCoalescesABurst(t *testing.T) {
	w, root := newWatcher(t)
	path := filepath.Join(root, "-a-project", "busy.jsonl")

	// A single turn appends many lines; that must not become many signals.
	for range 20 {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		if _, err := f.WriteString(`{"type":"user"}` + "\n"); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	}
	if !waitForChange(t, w) {
		t.Fatal("the burst should be reported once")
	}
	select {
	case <-w.Changes():
		t.Error("a settled burst should produce a single signal")
	case <-time.After(settle * 2):
	}
}

func TestWatcherNoticesAProjectCreatedLater(t *testing.T) {
	w, root := newWatcher(t)
	fresh := filepath.Join(root, "-brand-new")
	if err := os.MkdirAll(fresh, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if !waitForChange(t, w) {
		t.Fatal("a new project directory should be reported")
	}
	// And the transcripts inside it must be watched from then on.
	if err := os.WriteFile(filepath.Join(fresh, "first.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !waitForChange(t, w) {
		t.Fatal("a transcript in a newly created project should be reported")
	}
}

func TestWatcherIgnoresUnrelatedFiles(t *testing.T) {
	w, root := newWatcher(t)
	if err := os.WriteFile(filepath.Join(root, "-a-project", "notes.txt"), []byte("hi"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case <-w.Changes():
		t.Error("a non-transcript file should not wake the map")
	case <-time.After(settle * 2):
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	w, _ := newWatcher(t)
	if err := w.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	_ = w.Close() // must not panic on a second call
}

package artifacts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tree writes files at the given relative paths, each holding size bytes.
func tree(t *testing.T, root string, sizes map[string]int) {
	t.Helper()
	for rel, size := range sizes {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
}

func TestReadWeighsDirectoriesByWhatIsUnderThem(t *testing.T) {
	root := t.TempDir()
	tree(t, root, map[string]int{
		"tmp/huge.json":            5000,
		"tmp/small.txt":            10,
		"tmp/deep/a/b/c/buried.js": 2000,
		"state.json":               50,
	})

	entries, err := Read(root, func(name string) bool { return name == "state.json" })
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("read %d entries, want tmp and state.json", len(entries))
	}
	// Heaviest first: the reason to open this screen is to find the weight.
	if entries[0].Name != "tmp" {
		t.Errorf("first entry is %q, want the heavy directory first", entries[0].Name)
	}
	if got := entries[0].Bytes; got != 7010 {
		t.Errorf("tmp weighs %d, want everything beneath it (7010)", got)
	}
	if got := entries[0].Files; got != 3 {
		t.Errorf("tmp holds %d files, want 3", got)
	}
	if !entries[0].Dir {
		t.Error("tmp is not marked as a directory")
	}
	if !entries[1].Reserved {
		t.Error("state.json is not marked reserved")
	}

	// Descending gives the level below, again weighed recursively.
	inner, err := Read(filepath.Join(root, "tmp"), nil)
	if err != nil {
		t.Fatalf("Read tmp: %v", err)
	}
	if len(inner) != 3 || inner[0].Name != "huge.json" {
		t.Fatalf("tmp contains %+v", inner)
	}
	if inner[1].Name != "deep" || inner[1].Bytes != 2000 {
		t.Errorf("deep weighs %d bytes at %q, want 2000", inner[1].Bytes, inner[1].Name)
	}
}

func TestJobsAndOrphans(t *testing.T) {
	root := t.TempDir()
	tree(t, root, map[string]int{
		"aaaa1111/tmp/big.json": 4000,
		"bbbb2222/tmp/x":        10,
		"cccc3333/tmp/y":        20,
	})
	// A stray file beside the directories is not a job.
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	jobs, err := Jobs(root)
	if err != nil {
		t.Fatalf("Jobs: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("found %d jobs, want 3", len(jobs))
	}
	if jobs[0].ID != "aaaa1111" || jobs[0].Bytes != 4000 {
		t.Errorf("heaviest job is %+v", jobs[0])
	}

	// Matching is by prefix: a directory is named by the start of its session ID.
	known := []string{"aaaa1111-0000-4000-8000-000000000001"}
	orphans := Orphans(jobs, known)
	if len(orphans) != 2 {
		t.Fatalf("found %d orphans, want the two whose conversation is gone", len(orphans))
	}
	for _, o := range orphans {
		if o.ID == "aaaa1111" {
			t.Error("a job whose conversation still exists was called an orphan")
		}
	}
}

// A machine that has never run a job has no directory, which is not a fault.
func TestJobsToleratesNoRoot(t *testing.T) {
	jobs, err := Jobs(filepath.Join(t.TempDir(), "never-existed"))
	if err != nil || jobs != nil {
		t.Errorf("Jobs on a missing root = %v, %v", jobs, err)
	}
}

// Work products reach hundreds of megabytes, so only the head is read — and a
// database rendered as characters is a thousand screens of noise, so data is
// named rather than returned.
func TestHeadReadsOnlyTheHeadAndNamesData(t *testing.T) {
	dir := t.TempDir()

	big := filepath.Join(dir, "listing.txt")
	line := strings.Repeat("a", 99) + "\n"
	if err := os.WriteFile(big, []byte(strings.Repeat(line, 5000)), 0o600); err != nil {
		t.Fatal(err)
	}
	peek, err := Head(big, 1000)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if peek.Read != 1000 {
		t.Errorf("read %d bytes, want the limit", peek.Read)
	}
	if peek.Total != int64(len(line)*5000) {
		t.Errorf("total = %d", peek.Total)
	}
	if !peek.Truncated() {
		t.Error("a file longer than the limit is not reported as truncated")
	}
	if len(peek.Text) != 1000 {
		t.Errorf("text is %d bytes, want only what was read", len(peek.Text))
	}

	// A short file comes back whole and is not called truncated.
	small := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(small, []byte("two words\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	peek, err = Head(small, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if peek.Truncated() || peek.Text != "two words\n" {
		t.Errorf("small file = %+v", peek)
	}

	// Data is detected and no text is handed back for it.
	for name, body := range map[string][]byte{
		"db.sqlite": append([]byte("SQLite format 3"), 0x00, 0x04, 0x00, 0x01),
		"mystery":   {0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8},
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		peek, err := Head(path, 1000)
		if err != nil {
			t.Fatal(err)
		}
		if !peek.Binary {
			t.Errorf("%s was not detected as data", name)
		}
		if peek.Text != "" {
			t.Errorf("%s handed back text: %q", name, peek.Text)
		}
	}

	// An empty file is text, and empty.
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if peek, err := Head(empty, 1000); err != nil || peek.Binary || peek.Text != "" {
		t.Errorf("empty file = %+v, %v", peek, err)
	}

	// A directory and a missing file are refused, not guessed at.
	if _, err := Head(dir, 1000); err == nil {
		t.Error("a directory was read as a file")
	}
	if _, err := Head(filepath.Join(dir, "nope"), 1000); err == nil {
		t.Error("a missing file was read")
	}
}

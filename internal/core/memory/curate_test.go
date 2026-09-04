package memory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// curateSet lays out a set with the drift that occurs in practice: a memory the
// index omits, a row whose file is gone, and links between them.
func curateSet(t *testing.T) Location {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "memory")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	entry := func(name, desc, body string) string {
		return "---\nname: " + name + "\ndescription: " + desc +
			"\nmetadata:\n  type: project\n  originSessionId: abc\n---\n\n" + body + "\n"
	}
	write("shard-manifest.md", entry("shard-manifest", "why twice",
		"See [[reader-contract]] and again [[reader-contract]]."))
	write("reader-contract.md", entry("reader-contract", "the contract", "Read before write."))
	write("alerting-inventory.md", entry("alerting-inventory", "what alerts",
		"Two systems. Related: [[reader-contract]]."))
	write(IndexFile, "# Memory index\n\n"+
		"- [Shard manifest](shard-manifest.md) — why twice\n"+
		"- [Reader contract](reader-contract.md) — the contract\n"+
		"- [Long gone](removed-long-ago.md) — not here any more\n")
	return Location{Project: "demo", Dir: dir}
}

func indexBody(t *testing.T, loc Location) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(loc.Dir, IndexFile))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// Deleting takes the row with the file. A row pointing at nothing is the state
// a session actually reads.
func TestRemoveTakesTheIndexRowWithIt(t *testing.T) {
	loc := curateSet(t)
	var binned []string
	if err := Remove(loc, "shard-manifest", func(path string) error {
		binned = append(binned, path)
		return os.Remove(path)
	}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(binned) != 1 || filepath.Base(binned[0]) != "shard-manifest.md" {
		t.Errorf("discarded %v", binned)
	}
	if got := indexBody(t, loc); strings.Contains(got, "shard-manifest") {
		t.Errorf("the index still lists it:\n%s", got)
	}
	// The rows it did not touch are unchanged, hook and all.
	if got := indexBody(t, loc); !strings.Contains(got, "- [Reader contract](reader-contract.md) — the contract") {
		t.Errorf("another row was rewritten:\n%s", got)
	}
	if err := Remove(loc, "never-existed", nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("Remove of a missing memory = %v", err)
	}
}

// Renaming follows the name everywhere it was used. A rename that leaves links
// pointing at nothing has traded one tidy name for several broken references.
func TestRenameFollowsTheNameEverywhere(t *testing.T) {
	loc := curateSet(t)
	relinked, err := Rename(loc, "reader-contract", "reader-ordering")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if relinked != 3 {
		t.Errorf("rewrote %d links, want 3", relinked)
	}
	if _, err := os.Stat(filepath.Join(loc.Dir, "reader-ordering.md")); err != nil {
		t.Errorf("the file was not renamed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(loc.Dir, "reader-contract.md")); err == nil {
		t.Error("the old file is still there")
	}
	set, err := Read(loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Dangling()) != 0 {
		t.Errorf("the rename left dangling links: %v", set.Dangling())
	}
	// The frontmatter agrees with the filename.
	body, err := os.ReadFile(filepath.Join(loc.Dir, "reader-ordering.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "name: reader-ordering") {
		t.Errorf("the frontmatter still names the old slug:\n%s", body)
	}
	// The index row moved with it, keeping its title and hook.
	if got := indexBody(t, loc); !strings.Contains(got, "(reader-ordering.md) — the contract") {
		t.Errorf("the index row did not follow:\n%s", got)
	}

	// Refusals, each for a reason.
	if _, err := Rename(loc, "reader-ordering", "Not A Slug"); err == nil {
		t.Error("a name that cannot be a filename was accepted")
	}
	if _, err := Rename(loc, "reader-ordering", "shard-manifest"); err == nil {
		t.Error("renaming onto an existing memory was accepted")
	}
	if _, err := Rename(loc, "never-existed", "whatever"); !errors.Is(err, ErrNotFound) {
		t.Errorf("renaming a missing memory = %v", err)
	}
}

// Repair makes the index agree with the files, in both directions.
func TestRepairMakesTheIndexAgreeWithTheFiles(t *testing.T) {
	loc := curateSet(t)
	before, err := Read(loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Unlisted()) != 1 || len(before.Orphaned) != 1 {
		t.Fatalf("the fixture is not drifted as expected: %+v", before)
	}

	added, dropped, err := Repair(loc)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if added != 1 || dropped != 1 {
		t.Errorf("added %d, dropped %d; want one of each", added, dropped)
	}
	after, err := Read(loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Unlisted()) != 0 {
		t.Errorf("still unlisted: %v", after.Unlisted())
	}
	if len(after.Orphaned) != 0 {
		t.Errorf("still orphaned: %v", after.Orphaned)
	}
	// The new row carries the memory's own description as its hook.
	if got := indexBody(t, loc); !strings.Contains(got, "(alerting-inventory.md) — what alerts") {
		t.Errorf("the added row has no hook:\n%s", got)
	}
	// Rows that were already right are untouched.
	if got := indexBody(t, loc); !strings.Contains(got, "- [Shard manifest](shard-manifest.md) — why twice") {
		t.Errorf("an existing row was rewritten:\n%s", got)
	}
	// Nothing to do the second time.
	if added, dropped, err := Repair(loc); err != nil || added != 0 || dropped != 0 {
		t.Errorf("second Repair = %d, %d, %v; want no work", added, dropped, err)
	}
}

// The index is what a session loads, so it is replaced atomically and stays
// private like everything else braids writes.
func TestIndexIsWrittenSafely(t *testing.T) {
	loc := curateSet(t)
	if _, _, err := Repair(loc); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(loc.Dir, IndexFile))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("the index is mode %o, readable beyond its owner", perm)
	}
	// No temporary file left behind.
	entries, err := os.ReadDir(loc.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".memory-index-") {
			t.Errorf("a temporary index was left behind: %s", e.Name())
		}
	}
	// The header survives.
	if !strings.HasPrefix(indexBody(t, loc), "# Memory index\n") {
		t.Errorf("the index lost its heading:\n%s", indexBody(t, loc))
	}
}

// Curation deletes and moves files, so a name that is not one memory inside
// the set is refused at the destructive call — not in whichever caller happens
// to supply it. "../precious" joined to a directory is a path outside it.
func TestCurationRefusesANameThatIsNotOneOfItsOwn(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "precious.md")
	if err := os.WriteFile(outside, []byte("---\nname: precious\n---\n\nkeep me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "memory")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "kept.md"),
		[]byte("---\nname: kept\ndescription: d\nmetadata:\n  type: project\n---\n\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, IndexFile),
		[]byte("# Memory index\n\n- [Kept](kept.md) — d\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loc := Location{Project: "p", Dir: dir}

	for _, name := range []string{
		"../precious", "../../precious", "/etc/passwd", "sub/dir", "", ".", "..",
		"Not A Slug", "trailing-", "UPPER",
	} {
		if err := Remove(loc, name, func(string) error {
			t.Fatalf("Remove(%q) reached the point of deleting something", name)
			return nil
		}); err == nil {
			t.Errorf("Remove(%q) was allowed", name)
		}
		if _, err := Rename(loc, name, "somewhere"); err == nil {
			t.Errorf("Rename(from=%q) was allowed", name)
		}
		if _, err := Rename(loc, "kept", name); err == nil {
			t.Errorf("Rename(to=%q) was allowed", name)
		}
	}

	// Nothing outside was touched, and the memory that was there still is.
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("a file outside the memory directory was affected: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "kept.md")); err != nil {
		t.Errorf("the memory in the set was affected: %v", err)
	}
}

// These are the harness's files. braids edits them; it does not adopt them, so
// it leaves their permissions as it found them.
func TestCurationKeepsTheModeItFound(t *testing.T) {
	loc := curateSet(t)
	for _, name := range []string{IndexFile, "shard-manifest.md", "reader-contract.md"} {
		if err := os.Chmod(filepath.Join(loc.Dir, name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Rename(loc, "reader-contract", "reader-ordering"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, _, err := Repair(loc); err != nil {
		t.Fatalf("Repair: %v", err)
	}
	for _, name := range []string{IndexFile, "shard-manifest.md", "reader-ordering.md"} {
		info, err := os.Stat(filepath.Join(loc.Dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o644 {
			t.Errorf("%s is now mode %o, was 0644 before braids touched it", name, got)
		}
	}

	// A file braids creates is braids' own to set.
	fresh := filepath.Join(t.TempDir(), "memory")
	if err := os.MkdirAll(fresh, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fresh, "only.md"),
		[]byte("---\nname: only\ndescription: d\nmetadata:\n  type: project\n---\n\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Repair(Location{Project: "p", Dir: fresh}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(fresh, IndexFile))
	if err != nil {
		t.Fatalf("Repair did not create the index it needed: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("an index braids created is mode %o, readable beyond its owner", perm)
	}
}

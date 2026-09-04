package memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// write lays down a memory directory shaped like a real one, including the two
// frontmatter shapes that occur in practice: with a recorded origin and
// modified time, and without either.
func write(t *testing.T, dir string, files map[string]string) Location {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return Location{Project: "demo", Dir: dir}
}

const withOrigin = `---
name: shard-manifest
description: "Why the manifest is written twice: the reader needs it before the writer finishes"
metadata:
  node_type: memory
  type: project
  originSessionId: c30bc218-edcc-4bf6-add4-238036cdb0ca
  modified: 2026-08-09T07:20:12.400Z
---

The manifest is written twice. See [[reader-contract]] and [[reader-contract]]
again, plus [[missing-note]].
`

const withoutOrigin = `---
name: no-ci-polling
description: Push and move on
metadata:
  type: feedback
---

**Why:** waiting on CI wastes a turn. Related: [[shard-manifest]].
`

func TestReadParsesBothFrontmatterShapes(t *testing.T) {
	loc := write(t, filepath.Join(t.TempDir(), "memory"), map[string]string{
		"shard-manifest.md":  withOrigin,
		"no-ci-polling.md":   withoutOrigin,
		"reader-contract.md": "---\nname: reader-contract\ndescription: x\nmetadata:\n  type: reference\n---\n\nbody\n",
		IndexFile: "# Memory index\n\n" +
			"- [Shard manifest](shard-manifest.md) — written twice\n" +
			"- [Reader contract](reader-contract.md) — the contract\n",
	})

	set, err := Read(loc)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(set.Memories) != 3 {
		t.Fatalf("read %d memories, want 3", len(set.Memories))
	}

	byName := map[string]Memory{}
	for _, m := range set.Memories {
		byName[m.Name] = m
	}

	shard := byName["shard-manifest"]
	if shard.Kind != "project" {
		t.Errorf("kind = %q, want project", shard.Kind)
	}
	if shard.Origin != "c30bc218-edcc-4bf6-add4-238036cdb0ca" {
		t.Errorf("origin = %q", shard.Origin)
	}
	// A description holding a colon and quotes has to survive intact.
	if want := "Why the manifest is written twice: the reader needs it before the writer finishes"; shard.Description != want {
		t.Errorf("description = %q, want %q", shard.Description, want)
	}
	if got := shard.Modified.UTC().Format(time.RFC3339); got != "2026-08-09T07:20:12Z" {
		t.Errorf("modified = %s", got)
	}
	if shard.Title != "Shard manifest" {
		t.Errorf("title = %q, want the index's", shard.Title)
	}
	// Links are unique and ordered by first appearance.
	if want := []string{"reader-contract", "missing-note"}; len(shard.Links) != 2 ||
		shard.Links[0] != want[0] || shard.Links[1] != want[1] {
		t.Errorf("links = %v, want %v", shard.Links, want)
	}

	// No recorded time falls back to the file's own, rather than to zero.
	if byName["no-ci-polling"].Modified.IsZero() {
		t.Error("a memory with no recorded time has no time at all")
	}
	if byName["no-ci-polling"].Kind != "feedback" {
		t.Errorf("kind = %q, want feedback", byName["no-ci-polling"].Kind)
	}
}

// The index decides what a session loads, so a memory missing from it exists
// and does nothing, and a row with no file points at nothing.
func TestReadFindsWhatTheIndexAndFilesDisagreeOn(t *testing.T) {
	loc := write(t, filepath.Join(t.TempDir(), "memory"), map[string]string{
		"shard-manifest.md": withOrigin,
		"no-ci-polling.md":  withoutOrigin,
		IndexFile: "# Memory index\n\n" +
			"- [Shard manifest](shard-manifest.md) — written twice\n" +
			"- [Gone](removed-long-ago.md) — not here any more\n",
	})
	set, err := Read(loc)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	unlisted := set.Unlisted()
	if len(unlisted) != 1 || unlisted[0].Name != "no-ci-polling" {
		t.Errorf("unlisted = %v, want just no-ci-polling", names(unlisted))
	}
	if len(set.Orphaned) != 1 || set.Orphaned[0] != "removed-long-ago" {
		t.Errorf("orphaned = %v, want removed-long-ago", set.Orphaned)
	}

	// reader-contract is absent here, so the link to it dangles, and so does
	// missing-note. shard-manifest names reader-contract twice and counts
	// once: a memory pointing at the same target twice is one relationship.
	dangling := set.Dangling()
	if len(dangling) != 2 {
		t.Errorf("dangling = %v, want two", dangling)
	}
	if got := set.Backlinks()["shard-manifest"]; got != 1 {
		t.Errorf("backlinks to shard-manifest = %d, want 1", got)
	}
	if got := set.ByKind(); got["project"] != 1 || got["feedback"] != 1 {
		t.Errorf("by kind = %v", got)
	}
	if set.Bytes() == 0 {
		t.Error("the set weighs nothing")
	}
}

// A project that has never remembered anything is empty, not an error.
func TestReadToleratesNoDirectory(t *testing.T) {
	set, err := Read(Location{Project: "p", Dir: filepath.Join(t.TempDir(), "never")})
	if err != nil || len(set.Memories) != 0 {
		t.Errorf("Read on a missing directory = %+v, %v", set, err)
	}
}

// Dirs finds one memory directory per project that has any.
func TestDirsFindsOnlyProjectsWithMemories(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "-Users-me-src-alpha", "memory"), map[string]string{
		"a.md": withoutOrigin,
	})
	if err := os.MkdirAll(filepath.Join(root, "-Users-me-src-beta"), 0o700); err != nil {
		t.Fatal(err)
	}
	locs, err := Dirs(root, func(slug string) string { return slug[len(slug)-5:] })
	if err != nil {
		t.Fatalf("Dirs: %v", err)
	}
	if len(locs) != 1 || locs[0].Project != "alpha" {
		t.Errorf("Dirs = %+v, want just alpha", locs)
	}
}

func names(ms []Memory) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.Name)
	}
	return out
}

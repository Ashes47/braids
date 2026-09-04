// Package memory reads the memories a harness keeps beside its transcripts.
//
// A memory is a small markdown file with frontmatter, and a directory of them
// has an index — MEMORY.md — that is what actually gets loaded into a session.
// So a memory can exist and do nothing, by being absent from the index, and an
// index row can point at a file that is no longer there. Reading both and
// comparing them is most of the value here.
//
// Everything in this package reads. Curation — deleting, renaming, retyping,
// repairing a link — would attach as functions beside these, mutating the same
// types, and would have one hard obligation: whatever it changes about a file,
// it changes about the index in the same breath, because the index is the part
// the harness reads.
package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// IndexFile is the name of the index that decides what a session actually
// loads.
const IndexFile = "MEMORY.md"

// Location is one project's memory directory.
type Location struct {
	// Project is the name braids shows conversations under.
	Project string
	Dir     string
}

// Memory is one remembered fact.
type Memory struct {
	// Name is the slug, which is both the filename and what links use.
	Name string
	// Title is what the index calls it, which may differ from the slug.
	Title       string
	Description string
	// Kind is the frontmatter's type: user, feedback, project or reference.
	// Harnesses may add others, so it is not an enumeration.
	Kind string
	// Origin is the session that wrote it, when it recorded one. It is the
	// edge worth having: it leads back to the conversation where the decision
	// was actually made.
	Origin   string
	Modified time.Time
	Path     string
	Bytes    int64
	// Links are the [[names]] it points at, in the order they first appear.
	Links []string
	// Listed is whether the index mentions it. A memory that is not listed is
	// never loaded, so it exists and does nothing.
	Listed bool
}

// Link is one memory pointing at another.
type Link struct{ From, To string }

// Set is a project's memories, and what the index says about them.
type Set struct {
	Location
	Memories []Memory
	// Orphaned are index rows naming a file that is not there.
	Orphaned []string
}

var (
	linkPattern  = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
	indexPattern = regexp.MustCompile(`^\s*[-*]\s*\[([^\]]*)\]\(([^)]+)\)`)
)

// Read loads one memory directory. A directory that is not there yet is an
// empty set rather than an error: a project simply has not remembered anything.
func Read(loc Location) (Set, error) {
	set := Set{Location: loc}
	entries, err := os.ReadDir(loc.Dir)
	if errPathMissing(err) {
		return set, nil
	}
	if err != nil {
		return set, fmt.Errorf("read %s: %w", loc.Dir, err)
	}

	titles, err := readIndex(filepath.Join(loc.Dir, IndexFile))
	if err != nil {
		return set, err
	}
	present := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == IndexFile || !strings.HasSuffix(name, ".md") {
			continue
		}
		memory, err := readMemory(filepath.Join(loc.Dir, name))
		if err != nil {
			return set, err
		}
		memory.Title, memory.Listed = titles[memory.Name], titles[memory.Name] != "" || listed(titles, memory.Name)
		present[memory.Name] = true
		set.Memories = append(set.Memories, memory)
	}
	for slug := range titles {
		if !present[slug] {
			set.Orphaned = append(set.Orphaned, slug)
		}
	}
	sort.Strings(set.Orphaned)
	sort.SliceStable(set.Memories, func(i, j int) bool {
		if !set.Memories[i].Modified.Equal(set.Memories[j].Modified) {
			return set.Memories[i].Modified.After(set.Memories[j].Modified)
		}
		return set.Memories[i].Name < set.Memories[j].Name
	})
	return set, nil
}

func errPathMissing(err error) bool {
	return err != nil && os.IsNotExist(err)
}

// listed reports whether the index mentions a slug at all, even with an empty
// title.
func listed(titles map[string]string, slug string) bool {
	_, ok := titles[slug]
	return ok
}

// readIndex reads MEMORY.md, mapping each row's slug to the title it gives.
func readIndex(path string) (map[string]string, error) {
	body, err := os.ReadFile(path)
	if errPathMissing(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	titles := map[string]string{}
	for _, line := range strings.Split(string(body), "\n") {
		match := indexPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		titles[strings.TrimSuffix(filepath.Base(match[2]), ".md")] = strings.TrimSpace(match[1])
	}
	return titles, nil
}

// readMemory parses one file: its frontmatter, and the links in its body.
func readMemory(path string) (Memory, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Memory{}, fmt.Errorf("read %s: %w", path, err)
	}
	memory := Memory{
		Name:  strings.TrimSuffix(filepath.Base(path), ".md"),
		Path:  path,
		Bytes: int64(len(body)),
	}
	front, rest := splitFrontmatter(string(body))
	fields := parseFrontmatter(front)
	if name := fields["name"]; name != "" {
		// The slug is the filename; a name that disagrees is what the file
		// says about itself, and the filename is what links resolve against.
		memory.Name = name
	}
	memory.Description = fields["description"]
	memory.Kind = fields["type"]
	memory.Origin = fields["originSessionId"]
	if at, err := time.Parse(time.RFC3339, fields["modified"]); err == nil {
		memory.Modified = at
	} else if info, err := os.Stat(path); err == nil {
		// No recorded time: the file's own is the honest fallback.
		memory.Modified = info.ModTime()
	}
	memory.Links = linksIn(front + rest)
	return memory, nil
}

// splitFrontmatter separates a leading --- block from the body.
func splitFrontmatter(text string) (front, rest string) {
	if !strings.HasPrefix(text, "---\n") {
		return "", text
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		return "", text
	}
	return text[4 : 4+end], text[4+end:]
}

// parseFrontmatter reads the flat and one-level-nested `key: value` pairs these
// files use. It is deliberately not a YAML parser: the shape is small and
// fixed, a dependency for it would be larger than the code, and a description
// containing a colon or quotes must survive, which is the only subtlety.
func parseFrontmatter(front string) map[string]string {
	fields := map[string]string{}
	for _, line := range strings.Split(front, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if value == "" {
			continue // a parent such as `metadata:`; its children follow
		}
		fields[key] = unquote(value)
	}
	return fields
}

// unquote strips one layer of surrounding quotes, as these files use for any
// value holding a colon.
func unquote(value string) string {
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'') {
			if out, err := strconv.Unquote(value); err == nil {
				return out
			}
			return value[1 : len(value)-1]
		}
	}
	return value
}

// linksIn returns the [[names]] a memory points at, once each, in the order
// they first appear.
func linksIn(text string) []string {
	var links []string
	seen := map[string]bool{}
	for _, match := range linkPattern.FindAllStringSubmatch(text, -1) {
		target := strings.TrimSpace(match[1])
		if target == "" || seen[target] {
			continue
		}
		seen[target] = true
		links = append(links, target)
	}
	return links
}

// Unlisted are memories the index does not mention. They are loaded by nothing,
// so they exist and do nothing — the failure that is invisible from inside a
// session, and the reason this screen is worth having.
func (s Set) Unlisted() []Memory {
	var out []Memory
	for _, m := range s.Memories {
		if !m.Listed {
			out = append(out, m)
		}
	}
	return out
}

// Dangling are links pointing at a memory that does not exist. A name that does
// not resolve yet is a legitimate note-to-self, so these are reported rather
// than treated as faults.
func (s Set) Dangling() []Link {
	names := make(map[string]bool, len(s.Memories))
	for _, m := range s.Memories {
		names[m.Name] = true
	}
	var out []Link
	for _, m := range s.Memories {
		for _, target := range m.Links {
			if !names[target] {
				out = append(out, Link{From: m.Name, To: target})
			}
		}
	}
	return out
}

// Backlinks counts how many memories point at each name, which is the closest
// thing to what a memory set is actually about.
func (s Set) Backlinks() map[string]int {
	counts := map[string]int{}
	for _, m := range s.Memories {
		for _, target := range m.Links {
			counts[target]++
		}
	}
	return counts
}

// ByKind counts the memories of each type.
func (s Set) ByKind() map[string]int {
	counts := map[string]int{}
	for _, m := range s.Memories {
		counts[m.Kind]++
	}
	return counts
}

// Bytes is what the set weighs.
func (s Set) Bytes() int64 {
	var total int64
	for _, m := range s.Memories {
		total += m.Bytes
	}
	return total
}

// Dirs finds the memory directories under a projects root, one per project
// that has remembered anything.
func Dirs(root string, project func(dir string) string) ([]Location, error) {
	entries, err := os.ReadDir(root)
	if errPathMissing(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", root, err)
	}
	var found []Location
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name(), "memory")
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		name := entry.Name()
		if project != nil {
			name = project(entry.Name())
		}
		found = append(found, Location{Project: name, Dir: dir})
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].Project < found[j].Project })
	return found, nil
}

// Fingerprint summarises a memory directory cheaply: how many memories it
// holds and when one last changed.
//
// It exists so search over memories can be live without re-reading them on
// every refresh. Reading 83 memories costs a few milliseconds, which is little
// but not nothing when it happens on every keystroke of a live session;
// stat-ing the directory costs nothing at all, and a memory is written by
// creating or rewriting a file, which both show here.
func Fingerprint(loc Location) (count int, newest time.Time) {
	entries, err := os.ReadDir(loc.Dir)
	if err != nil {
		return 0, time.Time{}
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		count++
		if info, err := entry.Info(); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	return count, newest
}

// Body reads a memory's text, without the frontmatter.
//
// Read deliberately does not load bodies: a listing needs names, kinds and
// links, and a set can hold hundreds. This is called for the one being read.
func Body(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	_, body := splitFrontmatter(string(raw))
	// splitFrontmatter hands back the closing --- with the body behind it.
	if rest, ok := strings.CutPrefix(strings.TrimLeft(body, "\n"), "---"); ok {
		body = rest
	}
	return strings.Trim(body, "\n"), nil
}

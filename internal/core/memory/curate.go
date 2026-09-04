package memory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Curation changes a memory set. Everything here obeys one rule: whatever it
// does to a file, it does to the index in the same breath.
//
// The index is what a session actually loads. A memory deleted without its row
// leaves a pointer to nothing; a memory renamed without its row becomes
// invisible while still sitting on disk. Both are failures you cannot see from
// inside a session, and both are what braids found in a real set the first time
// it looked — so they are not hypothetical, they are the normal outcome of
// editing these files by hand.

// slugPattern is what a memory may be called: the name is a filename and a link
// target, so it has to survive being both.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// ErrNotFound says a set holds no memory by that name.
var ErrNotFound = errors.New("no such memory")

// indexRow is one line of the index.
type indexRow struct {
	slug, title, hook string
	// raw is the line as it was written, kept so a row braids is not changing
	// goes back exactly as it came — including any shape this code does not
	// model.
	raw string
}

// Remove takes a memory out of a set. The file is handed to discard, which is
// how braids sends it to the bin rather than destroying it, and its index row
// goes with it.
func Remove(loc Location, name string, discard func(path string) error) error {
	path := filepath.Join(loc.Dir, name+".md")
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	header, rows, err := readRows(filepath.Join(loc.Dir, IndexFile))
	if err != nil {
		return err
	}
	kept := make([]indexRow, 0, len(rows))
	for _, row := range rows {
		if row.slug != name {
			kept = append(kept, row)
		}
	}
	// The index first: a row pointing at a file that is gone is a worse state
	// than a file with no row, because the row is what a session reads.
	if err := writeRows(filepath.Join(loc.Dir, IndexFile), header, kept); err != nil {
		return err
	}
	if discard == nil {
		return os.Remove(path)
	}
	return discard(path)
}

// Rename gives a memory a new name, and follows it everywhere the old name was
// used: the file, the index row, the frontmatter, and every link pointing at
// it. It reports how many links it rewrote.
//
// The links are the reason this is one operation rather than three. A rename
// that leaves fourteen memories pointing at a name that no longer exists has
// traded one tidy name for fourteen broken references.
func Rename(loc Location, from, to string) (relinked int, err error) {
	if !slugPattern.MatchString(to) {
		return 0, fmt.Errorf("%q is not a usable name: lowercase words joined by dashes", to)
	}
	if from == to {
		return 0, nil
	}
	fromPath := filepath.Join(loc.Dir, from+".md")
	toPath := filepath.Join(loc.Dir, to+".md")
	if _, err := os.Stat(fromPath); err != nil {
		return 0, fmt.Errorf("%w: %s", ErrNotFound, from)
	}
	if _, err := os.Stat(toPath); err == nil {
		return 0, fmt.Errorf("%s already exists", to)
	}

	// The frontmatter name is what the file says about itself, and must not
	// disagree with what it is called.
	body, err := os.ReadFile(fromPath)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", fromPath, err)
	}
	renamed := renameField(string(body), to)
	if err := os.WriteFile(fromPath, []byte(renamed), 0o600); err != nil {
		return 0, fmt.Errorf("write %s: %w", fromPath, err)
	}
	if err := os.Rename(fromPath, toPath); err != nil {
		return 0, fmt.Errorf("rename %s: %w", from, err)
	}

	// Then everything that referred to it.
	relinked, err = relink(loc, from, to)
	if err != nil {
		return relinked, err
	}
	header, rows, err := readRows(filepath.Join(loc.Dir, IndexFile))
	if err != nil {
		return relinked, err
	}
	for i, row := range rows {
		if row.slug != from {
			continue
		}
		rows[i].slug, rows[i].raw = to, ""
		if rows[i].title == "" {
			rows[i].title = titleFor(to)
		}
	}
	return relinked, writeRows(filepath.Join(loc.Dir, IndexFile), header, rows)
}

// Repair makes the index agree with the files: a row for every memory that has
// none, and no row for a file that is not there.
//
// An unlisted memory is the invisible failure — it exists, it is never loaded,
// and nothing inside a session can tell you. A row with no file is the reverse,
// and merely useless.
func Repair(loc Location) (added, dropped int, err error) {
	set, err := Read(loc)
	if err != nil {
		return 0, 0, err
	}
	header, rows, err := readRows(filepath.Join(loc.Dir, IndexFile))
	if err != nil {
		return 0, 0, err
	}
	present := make(map[string]bool, len(set.Memories))
	for _, m := range set.Memories {
		present[m.Name] = true
	}

	kept := make([]indexRow, 0, len(rows))
	listed := map[string]bool{}
	for _, row := range rows {
		if !present[row.slug] {
			dropped++
			continue
		}
		listed[row.slug] = true
		kept = append(kept, row)
	}
	// Appended in the order Read gives them, which is newest first, so a
	// repair does not shuffle rows a person put in an order.
	for _, m := range set.Memories {
		if listed[m.Name] {
			continue
		}
		added++
		kept = append(kept, indexRow{slug: m.Name, title: titleFor(m.Name), hook: m.Description})
	}
	if added == 0 && dropped == 0 {
		return 0, 0, nil
	}
	return added, dropped, writeRows(filepath.Join(loc.Dir, IndexFile), header, kept)
}

// relink rewrites every [[old]] as [[new]], across the whole set.
func relink(loc Location, from, to string) (int, error) {
	entries, err := os.ReadDir(loc.Dir)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", loc.Dir, err)
	}
	old, replacement := "[["+from+"]]", "[["+to+"]]"
	count := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") || entry.Name() == IndexFile {
			continue
		}
		path := filepath.Join(loc.Dir, entry.Name())
		body, err := os.ReadFile(path)
		if err != nil {
			return count, fmt.Errorf("read %s: %w", path, err)
		}
		hits := strings.Count(string(body), old)
		if hits == 0 {
			continue
		}
		if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(body), old, replacement)), 0o600); err != nil {
			return count, fmt.Errorf("write %s: %w", path, err)
		}
		count += hits
	}
	return count, nil
}

// renameField rewrites the frontmatter's own name.
func renameField(body, to string) string {
	front, rest := splitFrontmatter(body)
	if front == "" {
		return body
	}
	lines := strings.Split(front, "\n")
	for i, line := range lines {
		if key, _, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(key) == "name" {
			lines[i] = "name: " + to
			return "---\n" + strings.Join(lines, "\n") + rest
		}
	}
	return body
}

// titleFor makes a readable title out of a slug, for a row that needs one.
func titleFor(slug string) string {
	words := strings.ReplaceAll(slug, "-", " ")
	if words == "" {
		return slug
	}
	return strings.ToUpper(words[:1]) + words[1:]
}

// readRows reads the index as the lines above it and the rows themselves.
func readRows(path string) (header []string, rows []indexRow, err error) {
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []string{"# Memory index", ""}, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	seen := false
	for _, line := range strings.Split(strings.TrimRight(string(body), "\n"), "\n") {
		match := indexPattern.FindStringSubmatch(line)
		if match == nil {
			if !seen {
				header = append(header, line)
			}
			// A line between rows is not a row; dropping it is the price of
			// writing the file back in one shape.
			continue
		}
		seen = true
		slug := strings.TrimSuffix(filepath.Base(match[2]), ".md")
		hook := ""
		if _, after, found := strings.Cut(line, ")"); found {
			hook = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(after), "—-–"))
		}
		rows = append(rows, indexRow{slug: slug, title: strings.TrimSpace(match[1]), hook: hook, raw: line})
	}
	if len(header) == 0 {
		header = []string{"# Memory index", ""}
	}
	return header, rows, nil
}

// writeRows replaces the index atomically: a half-written index is a session
// that loads half a memory set.
func writeRows(path string, header []string, rows []indexRow) error {
	var out strings.Builder
	for _, line := range header {
		out.WriteString(line + "\n")
	}
	if len(header) > 0 && strings.TrimSpace(header[len(header)-1]) != "" {
		out.WriteString("\n")
	}
	for _, row := range rows {
		if row.raw != "" {
			out.WriteString(row.raw + "\n")
			continue
		}
		line := fmt.Sprintf("- [%s](%s.md)", row.title, row.slug)
		if row.hook != "" {
			line += " — " + row.hook
		}
		out.WriteString(line + "\n")
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".memory-index-*")
	if err != nil {
		return fmt.Errorf("create temp index: %w", err)
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // best effort once renamed

	if _, err := tmp.WriteString(out.String()); err != nil {
		return errors.Join(fmt.Errorf("write index: %w", err), tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close index: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return fmt.Errorf("restrict index: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("place index: %w", err)
	}
	return nil
}

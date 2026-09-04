// Package trash makes deletion reversible.
//
// Deleting a conversation moves its files aside rather than removing them, so
// the action can be undone. A conversation is self-contained — a fork holds its
// own copy of the prefix it shares — so nothing that survives a deletion
// depends on what was deleted.
package trash

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Item is one path that was moved, and where it went.
type Item struct {
	From string
	To   string
}

// Entry is everything moved by a single deletion, so one restore brings it all
// back. It is written beside the files it describes, because recovering a
// conversation days later cannot depend on the session that deleted it.
type Entry struct {
	ID    string    `json:"id"`
	Label string    `json:"label"`
	At    time.Time `json:"at"`
	Items []Item    `json:"items"`
	Bytes int64     `json:"bytes"`
}

// Retention is how long a deleted conversation is kept. Long enough that
// noticing the mistake a week later is still soon enough.
const Retention = 14 * 24 * time.Hour

// manifest is the file recording what an entry holds.
const manifest = "braids-deleted.json"

// Expires reports when an entry will be removed for good.
func (e Entry) Expires() time.Time { return e.At.Add(Retention) }

// Bin is a directory holding deleted files.
type Bin struct{ dir string }

// New returns a bin rooted at dir.
func New(dir string) *Bin { return &Bin{dir: dir} }

// Discard moves paths into the bin. Paths that do not exist are skipped, so a
// caller may offer everything a conversation might own without checking first.
func (b *Bin) Discard(label string, paths []string) (Entry, error) {
	entry := Entry{
		ID:    time.Now().UTC().Format("20060102-150405.000000"),
		Label: label,
		At:    time.Now(),
	}
	dest := filepath.Join(b.dir, entry.ID)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return Entry{}, fmt.Errorf("create bin: %w", err)
	}

	for _, from := range paths {
		info, err := os.Stat(from)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return entry, fmt.Errorf("stat %s: %w", from, err)
		}
		to := filepath.Join(dest, filepath.Base(from))
		if err := os.Rename(from, to); err != nil {
			// Put back what has already moved rather than leaving a
			// conversation half-deleted.
			return entry, errors.Join(fmt.Errorf("move %s: %w", from, err), b.Restore(entry))
		}
		entry.Items = append(entry.Items, Item{From: from, To: to})
		entry.Bytes += sizeOf(to, info)
	}
	if len(entry.Items) == 0 {
		return entry, os.Remove(dest)
	}
	if err := writeManifest(dest, entry); err != nil {
		return entry, err
	}
	return entry, nil
}

// List returns what the bin holds, most recently deleted first.
func (b *Bin) List() ([]Entry, error) {
	dirs, err := os.ReadDir(b.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read bin: %w", err)
	}
	var out []Entry
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		entry, err := readManifest(filepath.Join(b.dir, d.Name()))
		if err != nil {
			continue // an unreadable entry must not hide the rest
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out, nil
}

// Purge removes an entry for good.
func (b *Bin) Purge(id string) error {
	if err := validID(id); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(b.dir, id)); err != nil {
		return fmt.Errorf("purge %s: %w", id, err)
	}
	return nil
}

// Expire removes entries past their retention, returning how many went and how
// much they held. Called when the bin is opened, so the count shown is true.
func (b *Bin) Expire(now time.Time) (int, int64, error) {
	entries, err := b.List()
	if err != nil {
		return 0, 0, err
	}
	var gone int
	var bytes int64
	for _, e := range entries {
		if now.Before(e.Expires()) {
			continue
		}
		if err := b.Purge(e.ID); err != nil {
			return gone, bytes, err
		}
		gone++
		bytes += e.Bytes
	}
	return gone, bytes, nil
}

func writeManifest(dir string, entry Entry) error {
	body, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifest), body, 0o600); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

func readManifest(dir string) (Entry, error) {
	body, err := os.ReadFile(filepath.Join(dir, manifest))
	if err != nil {
		return Entry{}, fmt.Errorf("read manifest: %w", err)
	}
	var entry Entry
	if err := json.Unmarshal(body, &entry); err != nil {
		return Entry{}, fmt.Errorf("parse manifest: %w", err)
	}
	return entry, nil
}

// Restore puts an entry's files back where they came from.
func (b *Bin) Restore(entry Entry) error {
	var failures []error
	for _, item := range entry.Items {
		if err := os.MkdirAll(filepath.Dir(item.From), 0o755); err != nil {
			failures = append(failures, err)
			continue
		}
		if err := os.Rename(item.To, item.From); err != nil {
			failures = append(failures, fmt.Errorf("restore %s: %w", item.From, err))
		}
	}
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	return os.RemoveAll(filepath.Join(b.dir, entry.ID))
}

// RestoreByID brings back an entry the bin already holds.
func (b *Bin) RestoreByID(id string) (Entry, error) {
	if err := validID(id); err != nil {
		return Entry{}, err
	}
	entry, err := readManifest(filepath.Join(b.dir, id))
	if err != nil {
		return Entry{}, err
	}
	return entry, b.Restore(entry)
}

// sizeOf measures a path, walking a directory to total what it holds.
func sizeOf(path string, info fs.FileInfo) int64 {
	if !info.IsDir() {
		return info.Size()
	}
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // an unreadable entry contributes nothing
		}
		if fi, err := d.Info(); err == nil {
			total += fi.Size()
		}
		return nil
	})
	return total
}

// validID refuses anything that is not one plain directory name inside the bin.
// Purge deletes a whole tree, and filepath.Join happily resolves ".." out of the
// bin altogether — so the guard belongs here, at the destructive call, rather
// than in whichever caller happens to pass an ID today.
func validID(id string) error {
	if id == "" || id != filepath.Base(id) || id == "." || id == ".." ||
		strings.ContainsRune(id, filepath.Separator) {
		return fmt.Errorf("not a bin entry: %q", id)
	}
	return nil
}

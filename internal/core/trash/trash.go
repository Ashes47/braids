// Package trash makes deletion reversible.
//
// Deleting a conversation moves its files aside rather than removing them, so
// the action can be undone. A conversation is self-contained — a fork holds its
// own copy of the prefix it shares — so nothing that survives a deletion
// depends on what was deleted.
package trash

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Item is one path that was moved, and where it went.
type Item struct {
	From string
	To   string
}

// Entry is everything moved by a single deletion, so one undo restores it all.
type Entry struct {
	ID    string
	At    time.Time
	Items []Item
	Bytes int64
}

// Bin is a directory holding deleted files.
type Bin struct{ dir string }

// New returns a bin rooted at dir.
func New(dir string) *Bin { return &Bin{dir: dir} }

// Discard moves paths into the bin. Paths that do not exist are skipped, so a
// caller may offer everything a conversation might own without checking first.
func (b *Bin) Discard(paths []string) (Entry, error) {
	entry := Entry{ID: time.Now().UTC().Format("20060102-150405.000000"), At: time.Now()}
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
	return os.Remove(filepath.Join(b.dir, entry.ID))
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

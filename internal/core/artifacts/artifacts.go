// Package artifacts browses the work products a session left behind — the
// scratch files, clones and dumps a tool wrote while it worked.
//
// These outweigh the conversations by an order of magnitude: on one machine,
// 3.3 GB of work products against 363 MB of transcripts, and nine files
// account for 1.3 GB of it. So the browser reads one level at a time with
// directories weighed by everything beneath them, the way du and ncdu do.
// Listing twelve thousand files flat would be accurate and useless; what a
// person needs is to see which single branch of the tree is heavy and walk
// into it.
package artifacts

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Entry is one child of a directory: a file, or a directory weighed by
// everything under it.
type Entry struct {
	Name  string
	Path  string
	Dir   bool
	Bytes int64
	// Files is how many files it holds, 1 for a file itself. It separates one
	// enormous dump from a directory of ten thousand small things, which want
	// different decisions.
	Files int
	At    time.Time
	// Reserved marks something the harness owns rather than scratch a tool
	// wrote. Deleting a job's own record would break the harness; the browser
	// shows these so the tree adds up, and refuses to remove them.
	Reserved bool
}

// Reserve reports whether a name at the top of a job directory belongs to the
// harness rather than to the work. Nil means nothing is reserved.
type Reserve func(name string) bool

// Read lists one level of dir. Directory sizes are recursive, so the numbers
// add up to the parent and a heavy branch is visible without descending it.
func Read(dir string, reserved Reserve) ([]Entry, error) {
	children, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	entries := make([]Entry, 0, len(children))
	for _, child := range children {
		path := filepath.Join(dir, child.Name())
		entry := Entry{Name: child.Name(), Path: path, Dir: child.IsDir()}
		if reserved != nil {
			entry.Reserved = reserved(child.Name())
		}
		switch {
		case child.IsDir():
			entry.Bytes, entry.Files, entry.At = weigh(path)
		default:
			info, err := child.Info()
			if err != nil {
				// A file that vanished between listing and stat is not an
				// error: work products are written while braids is looking.
				continue
			}
			entry.Bytes, entry.Files, entry.At = info.Size(), 1, info.ModTime()
		}
		entries = append(entries, entry)
	}
	sortEntries(entries)
	return entries, nil
}

// sortEntries puts the heaviest first: the reason to open this screen is to
// find what is taking the room.
func sortEntries(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Bytes != entries[j].Bytes {
			return entries[i].Bytes > entries[j].Bytes
		}
		return entries[i].Name < entries[j].Name
	})
}

// weigh totals a directory: bytes, files, and the most recent change under it.
func weigh(dir string) (bytes int64, files int, at time.Time) {
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // an unreadable entry contributes nothing
		}
		info, err := d.Info()
		if err != nil {
			return nil //nolint:nilerr // same
		}
		bytes += info.Size()
		files++
		if info.ModTime().After(at) {
			at = info.ModTime()
		}
		return nil
	})
	return bytes, files, at
}

// Job is one session's directory of work products.
type Job struct {
	// ID is the short session ID the directory is named by.
	ID    string
	Path  string
	Bytes int64
	Files int
	At    time.Time
}

// Jobs lists every artifact directory under root, heaviest first. A missing
// root is not an error: a machine that has never run a job has none.
func Jobs(root string) ([]Job, error) {
	children, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", root, err)
	}
	jobs := make([]Job, 0, len(children))
	for _, child := range children {
		if !child.IsDir() {
			continue
		}
		path := filepath.Join(root, child.Name())
		bytes, files, at := weigh(path)
		jobs = append(jobs, Job{ID: child.Name(), Path: path, Bytes: bytes, Files: files, At: at})
	}
	sort.SliceStable(jobs, func(i, j int) bool {
		if jobs[i].Bytes != jobs[j].Bytes {
			return jobs[i].Bytes > jobs[j].Bytes
		}
		return jobs[i].ID < jobs[j].ID
	})
	return jobs, nil
}

// Orphans are the artifact directories whose conversation braids no longer
// knows about — work products of a conversation that has been deleted, which
// nothing will ever look at again and nothing else will ever reclaim.
//
// known is the set of session IDs that still exist. Matching is by prefix
// because a job directory is named by the first characters of its session ID.
func Orphans(jobs []Job, known []string) []Job {
	var orphans []Job
	for _, job := range jobs {
		if !claimed(job.ID, known) {
			orphans = append(orphans, job)
		}
	}
	return orphans
}

func claimed(id string, known []string) bool {
	for _, lane := range known {
		if len(lane) >= len(id) && lane[:len(id)] == id {
			return true
		}
	}
	return false
}

// File is one work product, named by its path relative to the top of the job.
type File struct {
	Rel   string
	Path  string
	Bytes int64
	At    time.Time
}

// vendored are directories whose contents nobody searches their own machine
// for. A node_modules holds thousands of files with names like index.js, and
// indexing them buries the one dump you were actually looking for.
var vendored = map[string]bool{
	"node_modules": true, ".git": true, "__pycache__": true,
	".venv": true, "venv": true, ".mypy_cache": true, ".pytest_cache": true,
	".next": true, ".cache": true, "site-packages": true,
}

// Files lists the work products under dir by relative path, skipping vendored
// directories. Names only: a work product can be 231 MB, and reading them to
// index their contents would cost more than everything else braids does put
// together.
func Files(dir string) ([]File, error) {
	var files []File
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable branch contributes nothing
		}
		if d.IsDir() {
			if vendored[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil //nolint:nilerr // vanished between listing and stat
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil //nolint:nilerr // outside the tree; not ours to report
		}
		files = append(files, File{Rel: rel, Path: path, Bytes: info.Size(), At: info.ModTime()})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", dir, err)
	}
	return files, nil
}

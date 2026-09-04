// Package watch reports when local transcripts change.
//
// Transcripts are append-only and written a line at a time, so a single turn
// produces a burst of events. The watcher coalesces a burst into one signal:
// callers only need to know that *something* moved, because bringing the index
// up to date is a cheap incremental sync rather than a rebuild.
package watch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// settle is how long the watcher waits for a burst of writes to stop before
// reporting it. Long enough to coalesce one turn, short enough to feel live.
const settle = 400 * time.Millisecond

// Watcher signals when anything under a transcript root changes.
type Watcher struct {
	fs      *fsnotify.Watcher
	changes chan struct{}
	done    chan struct{}
	once    sync.Once
}

// New starts watching each root and every directory immediately beneath it.
//
// braids watches two places: where the harness writes transcripts, and where
// sessions report what they are waiting on. Both change the map, so both wake
// it the same way.
func New(roots ...string) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("start watcher: %w", err)
	}
	w := &Watcher{
		fs:      fsw,
		changes: make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
	for _, root := range roots {
		if err := w.watchTree(root); err != nil {
			return nil, err
		}
	}
	go w.run()
	return w, nil
}

// Changes yields one value per settled burst of writes. It is buffered and
// lossy by design: a caller that is mid-refresh does not need to be told twice.
func (w *Watcher) Changes() <-chan struct{} { return w.changes }

// Close stops watching.
func (w *Watcher) Close() error {
	w.once.Do(func() { close(w.done) })
	return w.fs.Close()
}

// watchTree adds the root and its immediate project directories. fsnotify is
// not recursive, and transcripts live exactly one level down.
func (w *Watcher) watchTree(root string) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", root, err)
	}
	if err := w.fs.Add(root); err != nil {
		return fmt.Errorf("watch %s: %w", root, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("read %s: %w", root, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// A directory that vanishes between listing and watching is not an
		// error; it simply has nothing to report.
		_ = w.fs.Add(filepath.Join(root, e.Name()))
	}
	return nil
}

func (w *Watcher) run() {
	timer := time.NewTimer(settle)
	if !timer.Stop() {
		<-timer.C
	}
	pending := false

	for {
		select {
		case <-w.done:
			return

		case event, ok := <-w.fs.Events:
			if !ok {
				return
			}
			// A new project directory needs watching too, or the first
			// conversation in a fresh project would never be noticed.
			if event.Has(fsnotify.Create) && isDir(event.Name) {
				_ = w.fs.Add(event.Name)
			}
			if !interesting(event) {
				continue
			}
			if !pending {
				pending = true
			} else if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(settle)

		case <-timer.C:
			pending = false
			select {
			case w.changes <- struct{}{}:
			default: // a signal is already waiting; one is enough
			}

		case <-w.fs.Errors:
			// Watch errors are transient and not worth interrupting a session
			// for: the next change re-reports, and a manual sync always works.
		}
	}
}

// interesting filters out everything that is not a transcript or an event log
// moving. Both are JSONL, which is the whole test.
func interesting(event fsnotify.Event) bool {
	if event.Op == fsnotify.Chmod {
		return false
	}
	return strings.HasSuffix(event.Name, ".jsonl") || isDir(event.Name)
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

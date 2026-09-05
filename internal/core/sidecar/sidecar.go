// Package sidecar holds the small facts braids keeps beside the transcripts:
// where a branch came from, and what has been archived.
//
// None of it is written into a transcript — that is someone else's file format
// — and none of it is required. Losing a sidecar costs a name or a preference,
// never a conversation.
package sidecar

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Store is a small JSON map kept on disk, written whole on every change.
// Whole-file writes are the right trade here: these files hold tens of entries,
// and an atomic replace cannot leave a half-written one behind.
// It is safe for concurrent use. The map is read while bringing the index up
// to date, which happens off the interface thread so a slow read cannot freeze
// the frame, and written by whatever the person is doing at the same time. A Go
// map read and written at once does not return stale data — it crashes the
// program.
type Store[T any] struct {
	path    string
	mu      sync.RWMutex
	entries map[string]T
}

// Load reads a store, treating a missing or unreadable file as an empty one.
func Load[T any](path string) (*Store[T], error) {
	s := &Store[T]{path: path, entries: map[string]T{}}
	body, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) || len(body) == 0 {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(body, &s.entries); err != nil {
		// Corruption must not block the tool: everything here is an
		// improvement over inference, never a prerequisite for it.
		s.entries = map[string]T{}
	}
	return s, nil
}

// All returns a copy of every entry. A copy, because the caller may hold it
// while something else writes: these stores have tens of entries, so copying
// costs nothing next to the alternative of handing out shared state.
func (s *Store[T]) All() map[string]T {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]T, len(s.entries))
	for k, v := range s.entries {
		out[k] = v
	}
	return out
}

// Get reports an entry and whether it was present.
func (s *Store[T]) Get(key string) (T, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.entries[key]
	return v, ok
}

// Has reports whether an entry exists.
func (s *Store[T]) Has(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.entries[key]
	return ok
}

// Set records an entry and writes through immediately, so a crash between
// acting and quitting cannot lose it.
func (s *Store[T]) Set(key string, value T) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[key] = value
	return s.flush()
}

// Delete removes an entry.
func (s *Store[T]) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, key)
	return s.flush()
}

// flush writes the whole file. The caller holds the write lock.
func (s *Store[T]) flush() error {
	body, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", filepath.Base(s.path), err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(s.path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".sidecar-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // best effort once renamed

	if _, err := tmp.Write(body); err != nil {
		return errors.Join(fmt.Errorf("write %s: %w", s.path, err), tmp.Close())
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("flush %s: %w", s.path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", s.path, err)
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return fmt.Errorf("place %s: %w", s.path, err)
	}
	return nil
}

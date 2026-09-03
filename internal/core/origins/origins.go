// Package origins records where braids-made branches came from.
//
// It is a sidecar, never a change to a transcript: braids does not write into
// someone else's file format. Losing it costs accuracy on ambiguous forks, not
// data — the graph falls back to inference and every lane still resumes.
package origins

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Ashes47/braids/internal/core/model"
)

// Store is the recorded provenance of every branch braids has made.
type Store struct {
	path    string
	entries map[string]model.Origin
}

// Load reads the store, treating a missing file as an empty one.
func Load(path string) (*Store, error) {
	s := &Store{path: path, entries: map[string]model.Origin{}}
	body, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read origins: %w", err)
	}
	if len(body) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(body, &s.entries); err != nil {
		// A corrupt sidecar must not block the tool: provenance is an
		// optimisation over inference, never a prerequisite.
		s.entries = map[string]model.Origin{}
	}
	return s, nil
}

// All returns the recorded origins.
func (s *Store) All() map[string]model.Origin { return s.entries }

// Record saves where a lane came from, writing through immediately so a crash
// between branching and quitting cannot lose it.
func (s *Store) Record(laneID string, origin model.Origin) error {
	s.entries[laneID] = origin
	body, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encode origins: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create origins dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".origins-*")
	if err != nil {
		return fmt.Errorf("create temp origins: %w", err)
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // best effort once renamed

	if _, err := tmp.Write(body); err != nil {
		return errors.Join(fmt.Errorf("write origins: %w", err), tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close origins: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return fmt.Errorf("place origins: %w", err)
	}
	return nil
}

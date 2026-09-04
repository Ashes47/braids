package sidecar

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "archived.json")
	s, err := Load[bool](path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Has("a") {
		t.Error("a fresh store should be empty")
	}
	if err := s.Set("a", true); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Written through immediately, so a crash cannot lose it.
	again, err := Load[bool](path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if v, ok := again.Get("a"); !ok || !v {
		t.Errorf("entry did not survive: %v %v", v, ok)
	}

	if err := again.Delete("a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if third, err := Load[bool](path); err != nil || third.Has("a") {
		t.Error("deletion did not persist")
	}
}

func TestCorruptStoreDoesNotBlockTheTool(t *testing.T) {
	path := filepath.Join(t.TempDir(), "origins.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	s, err := Load[string](path)
	if err != nil {
		t.Fatalf("a corrupt sidecar should load as empty, got: %v", err)
	}
	if len(s.All()) != 0 {
		t.Error("want an empty store")
	}
	if err := s.Set("a", "b"); err != nil {
		t.Errorf("a corrupt store should still be writable: %v", err)
	}
}

func TestMissingFileIsAnEmptyStore(t *testing.T) {
	s, err := Load[int](filepath.Join(t.TempDir(), "absent.json"))
	if err != nil || len(s.All()) != 0 {
		t.Errorf("Load on a missing file = %v, %v", s, err)
	}
}

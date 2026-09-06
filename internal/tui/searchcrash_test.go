package tui

import (
	"testing"
	"time"

	"github.com/Ashes47/braids/internal/core/index"
	"github.com/Ashes47/braids/internal/core/memory"
)

// Reported from another machine: search a memory through global search, open
// it, press left a few times to go back, and the next key press took the whole
// session down with a nil dereference in searchKey.
//
// The cause was not the arrows. Every screen reachable from a hit remembers
// the mode it was opened from so that going back returns there, and the jump
// opened one while the mode was still searchMode after throwing the search
// away. Going back landed on a search screen with nothing behind it, and every
// key except esc read from that nothing.
func TestBackingOutOfAMemoryOpenedFromSearch(t *testing.T) {
	now := time.Now()
	lanes := []index.LaneInfo{
		laneInfo("aaaaaaaa-0000-4000-8000-000000000001", "import pipeline", "storefront", 100, time.Hour),
	}
	opts := Options{
		ASCII: true, Source: "claudecode",
		LoadMemories: func() ([]memory.Set, error) {
			return []memory.Set{{
				Location: memory.Location{Project: "storefront", Dir: "/m"},
				Memories: []memory.Memory{{
					Name: "shard-manifest", Description: "why twice", Kind: "project",
					Modified: now, Listed: true,
				}},
			}}, nil
		},
		Search: func(query, scope string) ([]index.Hit, error) {
			return []index.Hit{{
				Of: index.FoundMemory, Name: "shard-manifest",
				Project: "storefront", At: now,
			}}, nil
		},
	}
	m := NewModel(forestOf(lanes, nil), opts)
	m.now = func() time.Time { return now }
	m.width, m.height = 100, 24

	// The reported sequence, driven the way a terminal drives it.
	press := func(keys ...string) {
		t.Helper()
		for _, k := range keys {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("panic on %q: %v", k, r)
					}
				}()
				next, _ := m.Update(keyPress(k))
				m = next.(Model)
			}()
		}
	}
	press("/")
	press("s", "h", "a", "r", "d")
	press("enter") // open the memory
	press("left")  // out of the reader
	press("left")  // out of the memory list
	press("left")  // and again, which is where it died
	press("j", "k", "down", "tab", "x")

	// It must also land somewhere real rather than on a screen that is not there.
	if m.mode == searchMode && m.search == nil {
		t.Error("left the model in search mode with no search behind it")
	}
}

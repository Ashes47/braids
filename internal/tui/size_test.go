package tui

import (
	"testing"
	"time"

	"github.com/Ashes47/braids/internal/core/index"
)

// A terminal can be any size, including sizes nobody sensible would use: a
// split pane dragged to nothing, a tmux window mid-resize. A panic in a
// renderer takes the whole session with it, so every screen has to survive
// every size rather than only the ones the screenshots use.
func TestEveryScreenSurvivesAnySize(t *testing.T) {
	forest := forestOf([]index.LaneInfo{
		laneInfo("aaaaaaaa-0000-4000-8000-000000000001", "a conversation with a long title", "proj", 40, time.Hour),
		laneInfo("bbbbbbbb-0000-4000-8000-000000000002", "another", "proj", 3, 2*time.Hour),
	}, map[string]string{
		"bbbbbbbb-0000-4000-8000-000000000002": "aaaaaaaa-0000-4000-8000-000000000001",
	})

	for _, keys := range [][]string{
		nil, {"/"}, {"tab"}, {"enter"}, {"w"}, {"m"}, {"?"},
	} {
		for w := 1; w <= 50; w++ {
			for _, h := range []int{1, 2, 3, 8, 24} {
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("panic at %dx%d after keys %v:\n%v", w, h, keys, r)
						}
					}()
					RenderShot(forest, Options{}, Shot{Keys: keys, Width: w, Height: h})
				}()
			}
		}
	}
}

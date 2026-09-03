package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Ashes47/braids/internal/core/graph"
	"github.com/Ashes47/braids/internal/core/index"
	"github.com/Ashes47/braids/internal/core/model"
)

// Run assembles the forest from the index and starts the Map.
func Run(ctx context.Context, ix *index.Index, opts Options) error {
	if opts.LoadSpine == nil {
		opts.LoadSpine = SpineLoader(ctx, ix)
	}
	forest, err := Forest(ctx, ix, opts.Origins)
	if err != nil {
		return err
	}
	if len(forest.ByID) == 0 {
		return errors.New("no conversations indexed yet (run: braids index)")
	}
	if _, err := tea.NewProgram(NewModel(forest, opts), tea.WithContext(ctx)).Run(); err != nil {
		return fmt.Errorf("run map: %w", err)
	}
	return nil
}

// Render draws a single frame of the map and returns it. It exists so the map
// can be inspected without a terminal — for screenshots, for debugging, and
// for golden tests that would otherwise need a pty.
func Render(forest *graph.Forest, opts Options, laneID string, width, height int) string {
	m := NewModel(forest, opts)
	m.width, m.height = width, height
	m.clamp()
	if laneID == "" {
		return m.render()
	}
	for i, r := range m.visible {
		if strings.HasPrefix(r.node.Lane.ID, laneID) {
			m.cursor = i
			break
		}
	}
	m = m.openSpine()
	if m.spine == nil {
		return m.render()
	}
	return m.renderSpine()
}

// SpineLoader returns a loader that reduces a lane to its spine on demand.
func SpineLoader(ctx context.Context, ix *index.Index) func(string) ([]graph.Segment, error) {
	return func(laneID string) ([]graph.Segment, error) {
		msgs, err := ix.LaneMessages(ctx, laneID)
		if err != nil {
			return nil, err
		}
		return graph.Spine(msgs), nil
	}
}

// Forest reads everything the map needs and arranges it. Kept exported and
// separate from Run so the same assembly is reusable by a future web frontend.
func Forest(ctx context.Context, ix *index.Index, recorded map[string]model.Origin) (*graph.Forest, error) {
	lanes, err := ix.Lanes(ctx)
	if err != nil {
		return nil, err
	}
	overlaps, err := ix.Overlaps(ctx)
	if err != nil {
		return nil, err
	}
	timelines, err := ix.Timelines(ctx)
	if err != nil {
		return nil, err
	}
	return graph.Build(lanes, overlaps, timelines, recorded), nil
}

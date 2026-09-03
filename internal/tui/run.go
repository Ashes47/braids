package tui

import (
	"context"
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/Ashes47/braids/internal/core/graph"
	"github.com/Ashes47/braids/internal/core/index"
)

// Run assembles the forest from the index and starts the Map.
func Run(ctx context.Context, ix *index.Index, ascii bool) error {
	forest, err := Forest(ctx, ix)
	if err != nil {
		return err
	}
	if len(forest.ByID) == 0 {
		return errors.New("no conversations indexed yet (run: braids index)")
	}
	if _, err := tea.NewProgram(NewModel(forest, ascii), tea.WithContext(ctx)).Run(); err != nil {
		return fmt.Errorf("run map: %w", err)
	}
	return nil
}

// Forest reads everything the map needs and arranges it. Kept exported and
// separate from Run so the same assembly is reusable by a future web frontend.
func Forest(ctx context.Context, ix *index.Index) (*graph.Forest, error) {
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
	return graph.Build(lanes, overlaps, timelines), nil
}

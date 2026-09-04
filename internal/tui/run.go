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
	forest, err := Forest(ctx, ix, opts.Origins, opts.Names)
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

// Shot names one frame to draw, without a terminal.
//
// Screens past the map are reached with keys, so a shot carries the keys to
// press rather than a screen name. They go through the same router the running
// program uses and the frame comes from the same View, which is what keeps a
// screenshot honest: it is the screen, drawn by the code that draws the
// screen, not a second rendering written to resemble it.
type Shot struct {
	Lane   string   // conversation to put the cursor on first
	Query  string   // the search screen, for this query
	Keys   []string // pressed in order, once the cursor is placed
	Width  int
	Height int
}

// RenderShot draws one frame and returns it. It exists so the map can be
// inspected without a terminal: for the screenshots on braids.chat, for
// debugging, and for golden tests that would otherwise need a pty.
func RenderShot(forest *graph.Forest, opts Options, shot Shot) string {
	m := NewModel(forest, opts)
	m.width, m.height = max(shot.Width, minFrameWidth), max(shot.Height, minFrameHeight)
	m.clamp()
	if shot.Lane != "" {
		for i, r := range m.visible {
			if strings.HasPrefix(r.node.Lane.ID, shot.Lane) {
				m.cursor = i
				break
			}
		}
	}
	// Search is opened directly rather than by pressing "/", because the
	// router only claims that key when a search function is wired, and a
	// caller asking for the search screen means it.
	if shot.Query != "" {
		m = m.openSearch()
		for _, r := range shot.Query {
			m = m.searchKey(string(r))
		}
		return m.View().Content
	}
	var model tea.Model = m
	for _, key := range shot.Keys {
		model, _ = model.Update(keyPress(key))
	}
	return model.(Model).View().Content
}

// keyPress builds the message a keystroke arrives as. Named keys are spelled
// the way the hints spell them.
func keyPress(key string) tea.KeyPressMsg {
	switch key {
	case "enter", "↵":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	return tea.KeyPressMsg{Code: []rune(key)[0], Text: key}
}

// Render draws the map, one lane's spine, or the search screen. It is what the
// command line's --print calls, which needs no other screen.
func Render(forest *graph.Forest, opts Options, laneID, query string, width, height int) string {
	shot := Shot{Lane: laneID, Query: query, Width: width, Height: height}
	if query == "" && laneID != "" {
		shot.Keys = []string{"enter"}
	}
	return RenderShot(forest, opts, shot)
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
func Forest(ctx context.Context, ix *index.Index, recorded map[string]model.Origin, names map[string]string) (*graph.Forest, error) {
	lanes, err := ix.Lanes(ctx)
	if err != nil {
		return nil, err
	}
	// A name the user chose replaces whatever the harness called it, before
	// anything downstream — the map, the spine, search results — sees the lane.
	for i, l := range lanes {
		if name, ok := names[l.ID]; ok && name != "" {
			lanes[i].Title = name
		}
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

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"time"

	"github.com/Ashes47/braids/internal/core/index"
	"github.com/Ashes47/braids/internal/core/store"
)

// The machine surface is declared here as its own types rather than by tagging
// the ones braids works with internally. What it prints for a program is a
// promise that has to keep its shape; what it holds in memory should stay free
// to change without breaking a caller.
//
// IDs are whole here, always. The tables shorten them with an ellipsis to fit a
// terminal, which is right for a person glancing at a list and useless to
// anything that has to hand the ID back.

type laneOut struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Project  string    `json:"project"`
	Path     string    `json:"path"`
	Cwd      string    `json:"cwd,omitempty"`
	Messages int       `json:"messages"`
	Parts    int       `json:"parts"`
	Bytes    int64     `json:"bytes"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
	Resume   string    `json:"resume"`
}

func laneOf(l index.LaneInfo) laneOut {
	return laneOut{
		ID: l.ID, Title: l.Title, Project: l.Project, Path: l.Path, Cwd: l.Cwd,
		Messages: l.Messages, Parts: l.Parts, Bytes: l.Size,
		Created: l.Created, Updated: l.Updated,
		Resume: "claude --resume " + l.ID,
	}
}

type hitOut struct {
	Lane      string `json:"lane"`
	LaneTitle string `json:"lane_title"`
	Project   string `json:"project"`
	Message   string `json:"message"`
	// Turn is the position in the conversation, and what --at takes to branch
	// at this exact point.
	Turn    int       `json:"turn"`
	Kind    string    `json:"kind"`
	Role    string    `json:"role"`
	Tool    string    `json:"tool,omitempty"`
	Snippet string    `json:"snippet"`
	At      time.Time `json:"at"`
}

func hitOf(h index.Hit) hitOut {
	return hitOut{
		Lane: h.LaneID, LaneTitle: h.LaneTitle, Project: h.Project,
		Message: h.MessageID, Turn: h.Seq, Kind: string(h.Kind),
		Role: string(h.Role), Tool: h.Tool, Snippet: h.Snippet, At: h.At,
	}
}

type agentOut struct {
	ID string `json:"id"`
	// Lane is the conversation that spawned it.
	Lane     string `json:"lane"`
	Type     string `json:"type"`
	Task     string `json:"task"`
	Turn     int    `json:"turn"`
	Depth    int    `json:"depth"`
	Messages int    `json:"messages"`
	Path     string `json:"path"`
}

func agentOf(a index.SubagentRow) agentOut {
	return agentOut{
		ID: a.ID, Lane: a.LaneID, Type: a.Type, Task: a.Task,
		Turn: a.ParentSeq, Depth: a.Depth, Messages: a.Messages, Path: a.Path,
	}
}

// emit writes one JSON document. Indented, because these are read by a person
// debugging at least as often as by a program, and the size never makes that
// a cost worth saving.
func (p *printer) emit(v any) error {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	p.printf("%s\n", body)
	return p.Err()
}

// jsonFlag declares --json on a command. Declared in one place so every
// command spells it and describes it the same way.
func jsonFlag(fs *flag.FlagSet) *bool {
	return fs.Bool("json", false, "machine-readable output, with whole IDs")
}

// mergePlanOut reports both sides of a join. A caller deciding whether to merge
// needs the same two numbers a person does: what each side has that the other
// does not. Lane is empty for a plan, and the new conversation once joined.
func mergePlanOut(base, incoming string, plan store.MergePlan, lane string) any {
	return struct {
		Lane          string `json:"lane,omitempty"`
		Base          string `json:"base"`
		Incoming      string `json:"incoming"`
		BaseTurns     int    `json:"base_turns"`
		BaseOnlyTurns int    `json:"base_only_turns"`
		IncomingTurns int    `json:"incoming_turns"`
		Worthwhile    bool   `json:"worthwhile"`
	}{lane, base, incoming, plan.BaseTurns, plan.BaseOnlyTurns, plan.IncomingTurns, plan.Worthwhile()}
}

// orEmpty renders a nil slice as [] rather than null, so a caller can iterate
// what comes back without checking for it first.
func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

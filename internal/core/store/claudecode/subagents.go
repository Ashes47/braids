package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Ashes47/braids/internal/core/model"
)

// subagentMeta is what Claude Code records beside each sidechain transcript.
type subagentMeta struct {
	AgentType   string `json:"agentType"`
	Description string `json:"description"`
	ToolUseID   string `json:"toolUseId"`
	SpawnDepth  int    `json:"spawnDepth"`
}

// Subagents lists the conversations a lane spawned.
//
// They live one directory deeper than the lane, each with a meta file naming
// the tool call it answers — which is how a whole exchange is put back beside
// the single call the parent shows for it.
func (s *Source) Subagents(ctx context.Context, lane model.Lane) ([]model.Subagent, error) {
	dir := filepath.Join(strings.TrimSuffix(lane.Path, ".jsonl"), "subagents")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil // a lane that spawned nothing has no directory
	}
	if err != nil {
		return nil, fmt.Errorf("read subagents of %s: %w", lane.ID, err)
	}

	var out []model.Subagent
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(name, ".jsonl")
		path := filepath.Join(dir, name)

		agent := model.Subagent{ID: id, LaneID: lane.ID, Path: path}
		if meta, err := readSubagentMeta(filepath.Join(dir, id+".meta.json")); err == nil {
			agent.Type, agent.Task = meta.AgentType, meta.Description
			agent.ToolUseID, agent.Depth = meta.ToolUseID, meta.SpawnDepth
		}
		agent.Messages = countMessages(path)
		out = append(out, agent)
	}
	return out, nil
}

func readSubagentMeta(path string) (subagentMeta, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return subagentMeta{}, fmt.Errorf("read %s: %w", path, err)
	}
	var meta subagentMeta
	if err := json.Unmarshal(body, &meta); err != nil {
		return subagentMeta{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return meta, nil
}

// countMessages counts the conversational turns in a transcript. A subagent
// that cannot be read counts as empty rather than failing the lane around it.
func countMessages(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close() //nolint:errcheck // read-only

	n := 0
	sc := newScanner(f)
	for sc.Scan() {
		var r record
		if json.Unmarshal(sc.Bytes(), &r) != nil {
			continue
		}
		if _, ok := r.toMessage(""); ok {
			n++
		}
	}
	return n
}

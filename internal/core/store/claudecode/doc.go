// Package claudecode implements store.Source for Claude Code: JSONL transcripts
// under ~/.claude/projects, tailed by byte offset.
//
// Claude Code is the richest Source available: transcripts are already DAGs
// (uuid/parentUuid), forks preserve uuids so topology is exact, subagents live
// in a sidechain forest, and compact boundaries carry logicalParentUuid.
package claudecode

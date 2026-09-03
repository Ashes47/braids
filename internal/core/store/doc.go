// Package store defines the Source port: how braids reads an agent's local
// transcripts. Each supported harness ships a Source implementation; nothing
// above this package may assume a particular harness.
//
// A Source must be able to enumerate lanes, stream turns in order, and write a
// new transcript that the harness will resume. Everything else is optional and
// declared through Capabilities — in-file branching, subagent trees and
// compaction metadata exist in Claude Code and do not exist elsewhere.
package store

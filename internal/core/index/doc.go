// Package index maintains the SQLite FTS5 index over every message and tool
// call braids can see.
//
// The index holds no unique state: it is rebuilt from the Sources in seconds,
// so recovery is always "rebuild" rather than "repair".
package index

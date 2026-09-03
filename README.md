# braids

Claude Code works best on one linear thread doing one thing. Humans don't work
that way — a task forks, doubles back, and spawns three side quests. Today the
only way to cope is to run N terminals and hold the map in your head.

**braids is that map.** One TUI window alongside your N terminals. It shows every
conversation and every branch as a graph, searches all of them in microseconds,
and turns any message into the start of a new branch.

It never talks to Claude. It arranges the conversations you have with Claude.

## Status

Early. The index, search and the Map are working; the Spine is next. See
[SPEC.md](SPEC.md) for the full design and [§11](SPEC.md#11-build-order) for the
build order.

```
 Source:   claudecode                                          <j/k>   move
 Index:    ~/.braids/index.db                                  </>     filter
 Lanes:    21                                                  <g/G>   first/last
 Active:   9                                                   <q>     quit

╭─ Conversations(all)[21] ─────────────────────────────────────────────────────────╮
│  CONVERSATION                     FORK  PROJECT     TURNS      SIZE    AGE  STATUS│
│ ● git worktrees                         demo            6     57 kB     6m  active│
│ ├─● worktree cleanup              ← t6  demo            8     61 kB     6m  active│
│ └─● worktree vs clone             ← t4  demo            6     53 kB     6m  active│
│    └─● shallow clone              ← t6  demo            8     57 kB     6m  active│
│ ● Debug annotation pipeline …           microagi     1841      7 MB     5d    idle│
╰──────────────────────────────────────────────────────────────────────────────────╯
```

```
$ braids index
indexed 17 lanes · 59811 messages · 59839 searchable parts in 5.8s

$ braids search "gcsfuse density" --limit 3
Review annotation pipeline …  08-21 19:16  TaskCreate  {"subject":"Solve post [gcsfuse] [density] stall…
...
3 hits in 1.2ms
```

## Design in one screen

```
┌ braids ─────────────────────────────────────────────────────────── microagi ──┐
│  ◆ 1 needs you    ● 3 running    ○ 11 idle                    392 MB on disk  │
│                                                                               │
│  ● nvidia-delivery                            412 turns    running     2m ago │
│  ├─◆ try-option-c                    ← t412   38 turns    needs you    0m ago │
│  │ └─● cache-gate-probe              ← t31     7 turns    idle          3h ago│
│  ├─● gcsfuse-density                 ← t288   63 turns    done          2d ago│
│  └─● mdt-contention                  ← t104   21 turns    idle         14d ago│
├───────────────────────────────────────────────────────────────────────────────┤
│ / search   n needs-you   ↵ open   b branch   o terminal   a archive   ? help   │
└───────────────────────────────────────────────────────────────────────────────┘
```

## Running it

```sh
make build          # ./braids in the repo
./braids index      # scan ~/.claude/projects (a few seconds)
./braids            # open the map

make run            # build + open the map
make reindex        # rebuild the index
```

To put it on your PATH:

```sh
make install                            # -> $(go env GOPATH)/bin/braids
export PATH="$(go env GOPATH)/bin:$PATH"  # add to ~/.zshrc if not already there
braids
```

Other commands: `braids search QUERY [--kind text,tool_use] [--limit N]`,
`braids lanes`, `braids map --ascii`, `braids version`.

## Principles

1. **Files are the truth.** Everything is derived from `~/.claude/`. Delete
   braids and you lose a view, never a conversation.
2. **One writer per file, always.** Every branch is its own session file.
3. **The graph is for humans; each path is linear for the model.**
4. **Search is the front door; the graph is the confirmation.**
5. **A window you glance at, not a place you live.**

## License

MIT

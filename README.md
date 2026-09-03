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
$ braids
   CONVERSATION                                              TURNS      AGE
 ● Agent observability and branching conversations             297      30m
 ● Clarify halt vs force halt behavior                         279      52m
 ● Debug annotation pipeline dataset issue        9419fd9c   25571      52m
 ● Review AMI customer data delivery runbook      cec6177b   18264      52m
 j/k move   g/G ends   / filter   esc clear   q quit
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

## Principles

1. **Files are the truth.** Everything is derived from `~/.claude/`. Delete
   braids and you lose a view, never a conversation.
2. **One writer per file, always.** Every branch is its own session file.
3. **The graph is for humans; each path is linear for the model.**
4. **Search is the front door; the graph is the confirmation.**
5. **A window you glance at, not a place you live.**

## License

MIT

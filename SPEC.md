# braids — spec

> `github.com/Ashes47/braids` — a standalone open-source project, independent of
> any work repo.
>
> A terminal tool for managing Claude Code conversations as a graph:
> see every session and branch, search all of it instantly, and fork a new linear
> conversation from any message in any of them.

---

## 1. What it is

Claude Code works best on one linear thread doing one thing. Humans don't work
that way — a task forks, doubles back, and spawns three side quests. Today the
only way to cope is to run N terminals and hold the map in your head.

**braids is that map.** One TUI window alongside your N terminals. It shows every
conversation and every branch as a graph, lets you search all of them in
microseconds, and turns any message into the starting point of a new branch.

It never talks to Claude. It arranges the conversations you have with Claude.

---

## 2. Principles

1. **Files are the truth.** braids derives everything from `~/.claude/`. Delete
   braids and you lose a view, never a conversation.
2. **One writer per file, always.** Every branch is its own session file. braids
   never appends to a session someone else is using.
3. **The graph is for humans; each path is linear for the model.** Root-to-leaf
   is always a clean single-purpose context.
4. **Search is the front door; the graph is the confirmation.** Nobody finds a
   fork point by scrolling 3,400 turns.
5. **braids is a window you glance at, not a place you live.** Close it and six
   sessions keep running untouched.

---

## 3. Grounded facts

Every design decision below rests on something measured on this machine
(Claude Code 2.1.252, macOS), not assumed.

| Fact | Evidence |
|---|---|
| Transcripts are already DAGs | `uuid` + `parentUuid`; 228 branch points, fan-out 7 in one real session |
| Context = parent chain of the **last** record | Appending a node parented mid-file changed the model's history to `ALPHA, DELTA` |
| `leafUuid` does **not** control resume | Repointing it replayed the whole linear history |
| Fork from any node = one file write | Synthesized root→node path resumed as `ALPHA, BETA` |
| Fork mid-tool-call is safe | Harness drops the dangling `tool_use`, injects "Continue from where you left off" |
| Concurrent writers don't corrupt | Two parallel resumes produced a clean branch point |
| …but they race | A third resume inherited `ROOT, RIGHT`; `LEFT` was silently orphaned |
| Merge by splice works | Re-parented sibling branch → `ROOT, LEFT, RIGHT` in one context |
| Native forks preserve uuids | `--fork-session`: 11/11 shared records ⇒ **topology is free** |
| Fork files are standalone | Child holds a full copy of the prefix ⇒ **deletion never cascades** |
| Stale reads are guarded | Edit after external change: *"has been modified on disk since I read it"* |
| Local search is trivial | 60,616 units · 1.8 s full rebuild · 74.5 MB · **0.1–0.3 ms** queries |
| Subagents are a separate tree | `<sid>/subagents/agent-<id>.jsonl`, own root, joined by `toolUseId` |
| Subagents can be promoted | Cleared `isSidechain`, rewrote `sessionId` → resumed as a top-level session |
| Terminal identity is not portable | Warp exposes no session id ⇒ use `--name` for window titles, not focus APIs |
| Worktrees are native | `-w, --worktree [name]` |

---

## 4. Architecture

```
   ~/.claude/projects/**/*.jsonl ──┐
   ~/.claude/projects/*/*/subagents/*.jsonl ──┤
   ~/.claude/jobs/<sid>/{state.json,timeline.jsonl} ──┤
                                                      ├──▶  watcher (fsevents)
   hooks ── PermissionRequest ─ Notification ─┐        │       tail by byte offset
            Stop ─ SubagentStop ─ SessionStart┘        │            │
                    POST localhost ──────────────────▶ ├────────────┘
                                                       ▼
                                              index (SQLite + FTS5)
                                                       │
                                                       ▼
                                                   TUI render
                                                       │
                                        spawn ──▶ claude --resume … --name …
```

**Watcher.** Transcripts are append-only, so braids keeps a byte offset per file
and parses only the delta. A 145 MB file costs nothing to follow. Full rebuild
(1.8 s) is the cold start and the panic button.

**Index.** One SQLite file. FTS5 over `text | thinking | tool_use | tool_result`.
Rebuildable from scratch at any time; holds no unique state.

**Sidecar.** Names, colors, archive flags, worktree paths. Topology is *not*
stored — it is derived from shared uuids, so it survives losing the sidecar.

**Hooks.** braids installs its own hook entries (merging with existing ones, never
replacing) that POST session id + event to localhost. This is how liveness works
for sessions braids did not start.

**Launcher.** `claude --resume <id> --name <branch>` (+ `--worktree <branch>` for
workspace branches), spawned into a new window of the user's terminal.

---

### 4.1 Layering — TUI now, local web later

Three layers, and the boundary is enforced by imports:

```
   frontends   internal/tui  (bubbletea)      internal/web  (later: http + SSE)
                     │                                │
                     └──────────────┬─────────────────┘
   service                   internal/api           transport-neutral
                                    │               queries + commands + events
                     ┌──────────────┴─────────────────┐
   core        store · index · graph · ops · watch · events
```

Rules that keep the web option alive at zero cost:

- **Core never formats for a terminal.** No ANSI, no width math, no lipgloss
  types below `internal/api`. Core returns data; frontends decide presentation.
- **Every mutation is a named command** (`Branch`, `Clone`, `Promote`, `Archive`,
  `Delete`) with an explicit result — never logic buried in a key handler.
- **One typed event stream.** Core emits `LaneCreated`, `TurnAppended`,
  `NeedsYou`, `LaneIdle`. The TUI subscribes over a channel; the web gets the
  identical events over SSE. This is the decision that matters most — if
  liveness is wired into the TUI's update loop, the web has to reimplement it.
- **Watcher and hook listener live in core**, so `braids serve` doesn't duplicate
  them and a future daemon can host both frontends at once.
- **Read models are query-shaped, not screen-shaped**: `ListLanes(filter)`,
  `GetSpine(lane, zoom)`, `Search(q, scope)`. Both UIs call the same queries.

```
github.com/Ashes47/braids            ← go module path
├── cmd/braids/          TUI entry
├── internal/core/
│  ├── store/      jsonl parse, byte-offset tailing
│  ├── index/        sqlite + fts5
│  ├── graph/      lanes, junctions, fork detection by shared uuid
│  ├── ops/       branch, clone, promote, archive, delete
│  ├── watch/      fsnotify + hook http listener
│  └── events/     typed event bus
├── internal/api/    transport-neutral service ← the only thing UIs import
├── internal/tui/    bubbletea + lipgloss
└── internal/web/    later: handlers, SSE, embedded assets
```

This also yields a third frontend for free: a plain CLI (`braids search`,
`braids branch`) that is scriptable, useful before the TUI exists, and useful
forever from hooks and automation.

---

## 5. Data model

```
project
└── conversation            one .jsonl = one lane
    ├── turn                user↔assistant pair, the atom of the spine
    ├── run                 collapsed sequence of uninteresting turns
    ├── junction            turn with >1 child, or a shared-uuid fork
    └── subagent            separate tree, attached at its tool_use turn
```

**Edges**
- `parent` — `parentUuid`, within a file
- `fork` — derived: two conversations sharing a uuid; fork point = last shared
- `spawn` — `meta.json.toolUseId` → the parent's `tool_use` block
- `issued` — `sourceToolAssistantUUID`, tool_result → the assistant turn

**Statuses** — `running · needs-you · idle · done · archived`

### 5.1 Compaction breaks the chain — `logicalParentUuid` repairs it

`/compact` writes a `system` record with `subtype: compact_boundary` and
**`parentUuid: null`** — a new root — followed by a `user` record with
`isCompactSummary: true` carrying the summary text.

Measured here: one session has 5 chainless roots; 4 are compact boundaries, and
each carries a `logicalParentUuid` pointing at a record still present in the same
file. Across this history there are 35 compactions.

> **Walk `parentUuid ?? logicalParentUuid`.** Naive parent-chain walking splits
> one conversation into one lane per compaction — 35 phantom root lanes.

`compactMetadata` is a gift: `trigger`, `preTokens`, `postTokens`,
`cumulativeDroppedTokens`, `durationMs`, and the exact `preservedMessages.uuids`
carried across. One real boundary: **1,000,241 → 15,033 tokens, 985,208 dropped,
2m21s spent**. Render all of it; it is the most consequential thing that happens
to a conversation and today it is invisible.

**The dropped context is still in the file.** Compaction changes what is *sent*,
not what is *stored*. So branching from before a boundary recovers context the
running session has permanently lost — which makes a compact boundary the single
most valuable junction in the graph, and `b` on the seam is a headline feature.

---

## 6. Screens

### 6.1 The Map — home

```
┌ braids ─────────────────────────────────────────────────────────── microagi ──┐
│                                                                               │
│  ◆ 1 needs you    ● 3 running    ○ 11 idle                    392 MB on disk  │
│                                                                               │
│  ● nvidia-delivery                            412 turns    running     2m ago │
│  │    Debug annotation pipeline dataset issue                                 │
│  │                                                                            │
│  ├─◆ try-option-c                    ← t412   38 turns    needs you    0m ago │
│  │ │    permission · Bash(kubectl delete job dispatch-…)                      │
│  │ │                                                                          │
│  │ └─● cache-gate-probe              ← t31     7 turns    idle          3h ago│
│  │                                                                            │
│  ├─● gcsfuse-density                 ← t288   63 turns    done          2d ago│
│  │                                                                            │
│  └─● mdt-contention                  ← t104   21 turns    idle         14d ago│
│                                                                               │
│  ● schema-refactor                             89 turns    running    12m ago │
│  │                                                                            │
│  └─● models-ts-interfaces            ← t44    15 turns    idle          5h ago│
│                                                                               │
│  ○ 6 archived                                                             ▸   │
│                                                                               │
├───────────────────────────────────────────────────────────────────────────────┤
│ / search   n needs-you   ↵ open   b branch   o terminal   a archive   ? help  │
└───────────────────────────────────────────────────────────────────────────────┘
```

Vertical is sequence. Indent is fork depth. `← t412` is the exact turn the branch
left its parent. Idle age is on the right because staleness is the thing you scan
for. Archived lanes collapse to one line.

### 6.2 The Spine — inside one conversation

```
┌ nvidia-delivery ─────────────────────────────────────── 412 turns · running ──┐
│                                                                               │
│  ▲ turn 1 · 08-06 11:04                                              [g] top  │
│  ⋯ 180 turns                                                  47 Bash · 6 Edit│
│  │                                                                            │
│  ● t288  you     the gcsfuse mount is hard-coded to 10+1 per second           │
│  ├─● gcsfuse-density                                     63 turns · done      │
│  │                                                                            │
│  ⋯ 96 turns                                                                   │
│ ══ compacted · auto · 1,000,241 → 15,033 tok · 985,208 dropped · 2m21s ══ [b] │
│  ⌗ t396  summary   This session is being continued from a previous convers…   │
│  ⊕ t401  Explore · "find the ORDER BY key in the dispatcher"   6 turns    ▸   │
│  ⚠ t407  Bash exit 1 · kubectl get pods --context mirzakhani                  │
│  ⋯ 4 turns                                                                    │
│ ▓● t412  you     NULLS LAST starved the unstamped backlog — try option C      │
│  ├─◆ try-option-c                                       38 turns · needs you  │
│  │                                                                            │
│  ⋯ 8 turns                                                          idle 2m   │
│  ▼ turn 412 · 09-03 17:38                                            [G] end  │
│                                                                               │
├───────────────────────────────────────────────────────────────────────────────┤
│ ↑↓ move   ␣ expand   ↵ read   b branch here   / search here   esc map         │
└───────────────────────────────────────────────────────────────────────────────┘
```

93% of a real conversation is a straight line, so runs collapse to a single row
carrying a tool tally. Only rare things get weight: junctions, subagents, errors,
long idle gaps. `▓` is the cursor.

### 6.3 Search — the front door

```
┌ search ───────────────────────────────────────────────────────────────────────┐
│  › gcsfuse density▏                                  1,281 hits · 0.2 ms      │
│    scope  [all]  this convo   this branch     kind  all  you  claude  tool    │
├───────────────────────────────────────────────────────────────────────────────┤
│ ▸ nvidia-delivery    t288  you        the [gcsfuse] mount is hard-coded to 1  │
│   gcsfuse-density    t12   tool_use   gke-[gcsfuse]-tmp → gke-[gcsfuse]-buff  │
│   nvidia-delivery    t301  claude     …raising [density] past 10 needs the s  │
│   schema-refactor    t44   tool_res   mount option [density]=1 rejected by t  │
│   ○ mdt-contention   t9    you        does [gcsfuse] contend on the same MDT  │
├───────────────────────────────────────────────────────────────────────────────┤
│ ↵ jump to message   b branch from result   ⇥ scope   esc cancel               │
└───────────────────────────────────────────────────────────────────────────────┘
```

Typing re-highlights matching lanes on the map underneath, live, every keystroke.
At 0.2 ms that is free. `b` on a result branches directly from that message —
search → land → branch is the core loop, and it never touches the graph.

### 6.4 Needs-you — the switchboard

```
┌ needs you ─────────────────────────────────────────────────────── 3 waiting ──┐
│                                                                               │
│ ▸ try-option-c       permission    Bash(kubectl delete job dispatch-…)  0:42  │
│     window "try-option-c"                                                     │
│                                                                               │
│   schema-refactor    permission    Edit(models/session.ts)              3:15  │
│     window "schema-refactor"                                                  │
│                                                                               │
│   htx-delivery       notification  plan ready for review                8:01  │
│     window "htx-delivery"                                                     │
│                                                                               │
├───────────────────────────────────────────────────────────────────────────────┤
│ ↵ show me where   y copy resume cmd   esc back                                │
└───────────────────────────────────────────────────────────────────────────────┘
```

With six agents running, this is worth more than the graph. Every row names the
terminal window title, because `--name` makes titles reliable where focus APIs
are not.

### 6.5 Branch — inline at the junction, never a modal

```
│ ▓● t412  you     NULLS LAST starved the unstamped backlog — try option C      │
│  ├─┬ branch from turn 412                                                     │
│  │ │   name   try-option-c▏                                                   │
│  │ │   kind   ( ) thought      shares /Users/averma/Desktop/microagi          │
│  │ │          (•) workspace    git worktree ../microagi-try-option-c          │
│  │ │   open   (•) new terminal window      ( ) just create                    │
│  │ └   ↵ create      esc cancel                                               │
```

On create the new lane animates out of the junction and the terminal opens
already resumed. **thought** = shared cwd, exploratory. **workspace** = its own
worktree, safe to write, runs concurrently with siblings.

### 6.6 Subagent — expanded in place

```
│  ⊕ t401  Explore · "find the ORDER BY key in the dispatcher"   6 turns    ▾   │
│  ╎   ● you      Count the ORDER BY keys in the dispatcher and report          │
│  ╎   ● claude   Reading src/dispatch/queue.ts …                               │
│  ╎   ⚙ Grep     "ORDER BY" src/**/*.ts                        14 matches      │
│  ╎   ● claude   Three ORDER BY keys; only one is honored (queue.ts:212)       │
│  ╎                                             [p] promote to branch          │
```

Today this entire conversation is invisible — one `tool_use`/`tool_result` pair
in the parent. `p` promotes it to a first-class lane you can continue in.

### 6.7 Sweep — the Friday clean-up

```
┌ sweep ─────────────────────────────────────────────────────────────────────────┐
│  filter  idle > 30d   turns < 5   never branched from      12 lanes · 418 MB   │
├────────────────────────────────────────────────────────────────────────────────┤
│ [x] probe-mdt-contention        3 turns   idle 41d     2.1 MB                  │
│ [x] test-registry-tags          1 turn    idle 38d     0.4 MB                  │
│ [ ] halt-incident-postmortem   14 turns   idle 33d    61.0 MB   has 1 branch   │
│ [x] scratch-2026-07-19          2 turns   idle 46d     1.2 MB                  │
├────────────────────────────────────────────────────────────────────────────────┤
│  also reclaim   [x] job artifacts 3.2 GB    [ ] tool-result blobs 41 MB        │
├────────────────────────────────────────────────────────────────────────────────┤
│ ␣ toggle   A all   d delete   a archive   esc cancel                           │
└────────────────────────────────────────────────────────────────────────────────┘
```

Nobody deletes one lane; they clear thirty. Job artifacts are a separate axis —
on this machine they are 3.2 GB against 392 MB of actual conversation, so
"delete the work products, keep the thread" is the deletion nobody regrets.

### 6.8 Undo — deletion is quiet and reversible

```
│  ⌫ 4 lanes deleted · 3.6 GB reclaimed · child branches unaffected   [u] undo 9s│
```

No confirm dialog. Files move to `~/.braids/trash/<date>/`, purged after 7 days.
The toast states that children are unaffected, because that is the fear that
makes people hoard — and it is verifiably false here: every fork file carries a
full copy of its prefix.

---

## 7. Keymap

| Key | Action | Key | Action |
|---|---|---|---|
| `/` | search everything | `b` | branch from here |
| `n` | needs-you queue | `c` | clone lane / subtree |
| `j` `k` | move | `p` | promote subagent |
| `h` `l` | collapse / expand | `o` | open terminal for lane |
| `↵` | open lane / read message | `y` | copy resume command |
| `␣` | expand run or subagent | `a` | archive |
| `g` `G` | top / bottom | `d` | delete (undoable) |
| `⇥` | switch pane | `u` | undo |
| `esc` | zoom out one level | `f` | filter (incl. project) |
| `r` | rename lane | `m` | mark / multi-select |
| `1`–`9` | jump to lane | `?` | help |
| `q` | quit | `r` | rebuild index |

`esc` = go up a level, echoing Claude Code's own `esc esc` rewind.

---

## 8. Core interactions

**Branch from a message.** Walk `parentUuid` from the chosen node to the root,
write those records to a new session file with a fresh `sessionId`, append a
`last-prompt`, spawn `claude --resume <new> --name <branch>`. Original untouched.

**Clone a graph.** Copy each lane's file, rewrite `sessionId`, keep message uuids.
Topology reassembles itself, because forks are detected by shared uuids.

**Promote a subagent.** Set `isSidechain: false`, rewrite `sessionId`, drop
`agentId`, write as a top-level file.

**Merge (later).** Splice a sibling's unique nodes onto the current leaf with
fresh uuids. Real messages, not a summary. Gated behind a preview.

**Delete.** Move files to trash. Never cascades. Never allowed on a running lane.

**Branch a running lane — allowed.** Forking writes a new file, so the running
session is untouched. Permitted from any *completed* turn; only the in-flight
turn is blocked, because its records are still being written. This is the point:
you watch an agent go wrong, branch from three turns before it did, and try
another way without killing it.

**Auto-naming, no model involved.** Score the tokens of the forked message by
document frequency across the corpus — free, since the FTS5 index already has
it. Keep terms in a band (`2 ≤ df ≤ 4% of corpus`): dropping `df == 1` removes
typos, and the ceiling removes filler. Prefer tokens containing `- _ 0-9`, since
identifiers make better names. Take three, keep source order, slugify.

Validated against 2,298 real user messages here:

```
ttl-reconcile-provisioning   ← "Watcher misses the Running event (watch gap / ttl deletes…"
deployment-manual-right      ← "so we deployment is manual right now?"
clone-annotaion-fresh        ← "anther session is using this repo copy. In copies clone…"
start-caffeinate             ← "start caffeinate"
```

Good enough to never block on, not good enough to be permanent: `r` renames, and
names live in the sidecar, so renaming never touches a transcript.

**Worktree lifecycle — never automatic.** A conversation can be trashed and
undone; uncommitted code cannot. Archiving a workspace lane leaves its worktree
alone. Deleting offers removal as a *separate*, default-off checkbox, and refuses
outright when `git status --porcelain` is dirty or the branch has unpushed
commits — showing which, rather than a generic warning. Worktree state is a
column in the sweep screen.

---

## 9. Visual language

```
●  turn / lane            ⋯  collapsed run           ⊕  subagent
◆  needs you              ⚠  error or failed tool    ○  archived
├─ junction               ▓  cursor                  ╎  nested lane
▸ ▾  collapsed / expanded                            ←  fork point
```

- Colour carries **status only** — never identity. Identity is position and name.
- One accent for "needs you". Everything else is greyscale weight.
- Idle age is always right-aligned; it is the field people scan.
- Never render more than one junction per row.
- Animation is limited to a lane growing out of a junction on branch creation.
  That single motion is what teaches the model.

**Glyph width is a correctness issue.** `● ◆ ○ ← ⚠ ⊕` are East-Asian *Ambiguous*
width: some terminals and fonts render them double-wide, which silently breaks
every box in this document. Pick glyphs from the unambiguous-narrow set where
possible, measure with `go-runewidth` rather than `len()`, and add a
`--ascii` fallback (`* + o <- ! @`) for terminals that get it wrong. Golden-file
tests via `teatest` should assert frame width, not just content.

---

## 10. Non-goals

- Not a chat client. braids never sends a message to Claude.
- No server, no account, no sync. Everything is local.
- No auto-segmentation of existing conversations. **The user decides where a
  branch belongs** — the tool only makes it one keystroke.
- No re-stamping of stale environment on fork. A branch replays the world as it
  was; the harness already refuses edits based on stale reads.
- No merge in v1.

---

## 11. Build order

1. **Index + search.** Parse, FTS5, `/`. Useful alone on day one.
2. **The Map.** Lanes, fork detection by shared uuid, statuses from mtime.
3. **The Spine.** Runs, junctions, errors, read pane.
4. **Branch.** File synthesis + `--name` + `--worktree` + spawn.
5. **Live.** Watcher, byte-offset tailing, hooks, needs-you queue.
6. **Subagents.** Nested lanes, promote.
7. **Housekeeping.** Archive, sweep, trash, undo.
8. **Merge.** Splice with preview.

Steps 1–2 are already a tool worth opening.

---

## 12. Open questions

1. **Web frontend scope.** Deferred, not dismissed — §4.1 keeps it cheap. When it
   comes, does it mirror the TUI, or take a different shape entirely (wider
   canvas, real animation, mouse-first) for people who find a TUI hard?

Settled since first draft: compaction (§5.1), map scope — one global map with a
project prefix and `f` to filter, because the needs-you queue must never be
scoped away; branching a running lane (§8, allowed); worktree lifecycle (§8,
never automatic); auto-naming (§8, df-banded, renameable).

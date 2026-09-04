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
| …but not fork *direction* | A fork rewrites `sessionId` throughout and records no origin; timestamps are copied too, so nothing in the file says which came first |
| File birth time does say | Demo tree: root 23:21:00 → fork 23:21:20 → fork 23:21:39 → fork 23:21:49, all correct. APFS reports it; Linux may not |
| `parentUuid` names bookkeeping, not turns | A message's parent is usually an `attachment`; resolving through them is what makes a chain reconstruct at all |
| Worktree branches work end to end | `--worktree` put a resumed branch in `.claude/worktrees/<name>` on its own git branch; two of them edited the same file with neither touching the other or the main tree |
| Work products dwarf conversations | 3.5 GB of scratch and job records against 365 MB of transcript, all of it in two conversations |
| Job directories use the short ID | `~/.claude/jobs/9419fd9c`, not the full session UUID the transcript is filed under |
| 35 compactions across this history | Parsed for free: a boundary is announced one record before the summary that replaces what it dropped, so it rides the existing pass |
| In-file branching is the common case | One real 25,571-turn lane: **220 junctions, 228 departing branches** — none of them visible in Claude Code |
| A tool result wears the user role | Treating every user record as a landmark left a long spine 85% uncollapsed; requiring text cut 20,506 segments to 3,015 |
| A conversation's state is in its last turn | Assistant text ⇒ owed a reply; an assistant tool call with no result ⇒ outstanding, since the harness appends the result as a later turn |
| …but files cannot see a permission prompt | A running tool and one waiting for approval are the same record. Only a hook separates them |
| Identical prefixes make forks ambiguous | A lane cut from a fork shares the same turns with the fork *and* its parent; only a record of what braids did can tell them apart |
| Incremental sync is worth having | 9.2 s full rebuild vs **47 ms** when only one transcript moved |
| Stored mtimes are second-precision | Comparing a raw `ModTime` marks every lane changed forever, turning an incremental sync silently into a full one |
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

**Watcher.** fsnotify over the transcript root and each project directory,
adding new projects as they appear. A single turn appends many lines, so a burst
is coalesced into one signal after 400 ms of quiet — the map only needs to know
that *something* moved, because catching up is a 47 ms incremental sync. Watching
is a convenience, never a requirement: if it cannot start, the map opens as a
snapshot and says so by omitting `· live`.

**Index.** One SQLite file, stamped with a schema version. `Sync` re-reads only
lanes whose size or mtime moved and drops ones whose file is gone — 47 ms
against a 9.2 s full rebuild — which is what lets a branch appear immediately
instead of after a remembered command. Listing lanes costs a directory scan:
titles mean reading a whole transcript, so they are fetched only for lanes being
re-read. FTS5 over
`text | thinking | tool_use | tool_result`, plus a `messages` table carrying the
parent chain that the graph and the spine are derived from. Rebuildable from
scratch at any time; a version mismatch drops and recreates it.

**Sidecar.** Names, colors, archive flags, worktree paths, and the provenance of
branches braids made itself (`origins.json`). Topology is still *derived* — the
sidecar only overrides it where inference cannot win, because two lanes can hold
byte-identical prefixes and a third cut from either is indistinguishable by
content. Losing the sidecar costs accuracy on those ties, never data.

braids does not write into a transcript. Provenance could have been a record
inside the new file; a sidecar keeps someone else's format untouched.

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
- `parent` — `parentUuid`, within a file, **resolved through bookkeeping
  records**: Claude Code threads attachments and snapshots into the same chain,
  so a turn's raw parent is usually not a turn. A Source must report the nearest
  conversational ancestor or nothing downstream reconstructs
- `fork` — derived: two conversations sharing a uuid; fork point = last shared.
  *Direction* is decided by file birth time, because a fork copies the parent's
  records verbatim — timestamps included — and people routinely fork and then
  carry on in the original, so "diverged first" proves nothing. Where no birth
  time exists, weaker evidence in order: containment, then length, then
  last-touched
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
 Lane:      9419fd9c                                        <j/k>   move
 Turns:     25571                                           <g/G>   first/last
 Junctions: 220                                             <n/N>   next junction
 Branches:  228                                             <esc>   back to map

╭─ Spine(Debug annotation pipeline dataset issue)[3015] ─────────────────────────╮
│  TURN  WHO     WHAT HAPPENED                                           BRANCHES│
│ ●    t1 you     This session is being continued from a previous conver…        │
│ ⋯    t2         113 turns · 36 Bash · 2 ToolSearch · 1 AskUserQuestion         │
│ ●  t115 you     [Request interrupted by user for tool use]                     │
│ ●  t116 you     what's the status                                              │
│ ⋯  t117         10 turns · 4 Bash                                              │
│ ●  t127 you     what's the long term fix for this?                             │
│ ●  t128 claude  Long-term, this breaks into five fixes. Ordered by lev…        │
│   └─● try another way  (9 turns)                                          ← t128│
│ ●  t138 claude  Bash                                                      ├─ 2 │
╰────────────────────────────────────────────────────────────────────────────────╯
```

**A branch appears in both places, because they answer different questions.**
The map says *what exists*; the spine says *where this thread split*. A branch
that left at turn 128 is drawn inline there, indented under the turn it left
from — so reading a conversation shows you every point it diverged, without
going back to the map. `↵` descends into a branch and `esc` walks back up the
way you came, so the graph is navigable in both directions.

Two kinds of split share one vocabulary: a branch kept inside the transcript
(`├─ 2`, from a rewind) and one given its own file (`← t128`, from a fork). They
are the same event, and `n` / `N` steps through both.

The active path is the parent chain walked back from the **last** record, which
is exactly how the harness reconstructs context — so the spine shows the
conversation the model would see, not the file in order.

**Landmarks are human turns that said something.** A tool result is recorded
with the user role but is the harness returning output; counting it as a
landmark left a real 25,571-turn lane at 20,506 segments. Requiring text brought
it to 3,015. Everything between landmarks collapses to one line with a tool
tally, and a junction is always a landmark however dull the turn.

`n` / `N` step between **markers** — anywhere the conversation did something
other than carry on: a branch kept inside the transcript, a branch that left for
its own file, or an agent it spawned. Three things, one key, because scrolling a
320-row spine to find any of them is not navigation.

`j` / `k` and the arrows wrap: off the top is the bottom. Half-page jumps do not,
since landing at the far end of a long conversation loses the reader.

`/` filters the spine, matching a turn's text, its tools, its turn number and —
for a collapsed run — its summary, so filtering for `bash` finds the stretches
that used it. The field is the same one the map uses, so `/` behaves identically
on both screens, and `esc` peels one layer at a time: leave the field, clear the
text, then leave the screen.

**A compaction is drawn as a seam across the conversation**, naming what it let
go:

```
│ ●    t1 you     This session is being continued from a previous conversation…│
│ ══ compacted · auto · 1,000,939 → 14,569 tokens · 986,370 dropped · 2m45s ═══│
│ ⋯    t2         113 turns · 36 Bash · 2 ToolSearch                           │
```

It sits before the turn the conversation resumed at, not after the last one it
kept, because a collapsed run can swallow the turn a seam nominally follows and
the hole belongs where the thread picks up.

**`b` on a seam branches from the turn above it.** That is the point of drawing
them: compaction changes what is *sent*, never what is *stored*, so the turns
behind a seam are still in the file. Branching there recovers context the
running conversation has permanently lost — one real seam here dropped 986,370
tokens, and the largest 15.5 million.

Still not drawn: error marks on failed tool calls.

### 6.3 Search — the front door

```
 Query:    gcsfuse density                    <↵>     jump to turn <↑/↓>   move
 Scope:    every conversation                 <tab>   change scope <esc>   back
 Hits:     130
 Took:     22.8ms

╭─ Search(everywhere)[130] ──────────────────────────────────────────────────────╮
│ CONVERSATION              TURN KIND        MATCH                               │
│ Review annotation pip…   t2916 tool_result …Solve post [gcsfuse] [density] st…│
│ Review annotation pip…   t3467 tool_result …memory/[gcsfuse]-[density]-arc.md …│
│ Agent observability a…    t298 tool_result …[gcsfuse]-[density] t12 tool_use …│
╰────────────────────────────────────────────────────────────────────────────────╯
 /gcsfuse density▏  ↵ jump · tab scope · esc back
```

Full text over every message, thought and tool call — the same FTS5 index
`braids search` uses. It runs on every keystroke because it can: a query costs
milliseconds even over 60,000 rows.

**`↵` jumps to the turn, not to the result.** It opens the conversation with the
cursor on the matching turn — landing inside the thread is the point, since a
result on its own says nothing about what came before it. When the turn was
swallowed by a collapsed run, it lands on the run.

**`tab` changes scope.** Opened from a conversation, search starts inside it;
`tab` widens to everything and back.

`/` and `f` are different jobs and deserve different keys: `/` searches every
conversation, `f` narrows the list already in front of you.

### 6.4 Needs-you — the switchboard

Partly answered already, without hooks. A conversation's state is readable from
its last turn, so the map names it rather than guessing from file times:

| State | Meaning |
|---|---|
| `working` | a tool call is outstanding and the file is still moving |
| `thinking` | the last turn is yours and a reply is in flight |
| `your turn` | the assistant answered; nobody has replied |
| `stopped` | a tool call is outstanding but nothing has moved since — interrupted mid-call |
| `unanswered` | a prompt nobody ever answered, which is what cutting a branch at a question leaves |

`n` / `N` step between the conversations owed something by a person — a recent
`your turn`, a `stopped` call, an `unanswered` branch — and the facts block
counts them. That is the switchboard for everything except one case.

**What files cannot say** is whether an outstanding tool call is *running* or
*waiting for permission*: both are an assistant turn with no result yet. Naming
it `working` is the honest reading. A `PermissionRequest` or `Notification` hook
is what would let braids say `needs you` and mean it — and that, plus the
ordered queue below, is what remains of this screen.

### 6.5 Branch — inline at the junction, never a modal

```
│ ●   t5 you     Give one command to create a worktree.                          │
│   └─ branch from t5 as thought: create-worktree▏  tab switches kind · enter ·  │
```

The field opens on the turn it will cut, pre-filled from that turn's own words
(§8), and `tab` chooses what kind of branch it is:

- **thought** — shares the working directory. Exploring, reading, planning.
  The cheap default, because most branches never write anything.
- **workspace** — asks the harness for a git worktree of its own, so two
  branches that both write to the repo cannot collide.

braids does not create the worktree. `--worktree` is the harness's own flag and
its own job; braids records the choice and puts the flag in the resume command,
so `y` and `o` carry it. The kind is chosen fresh each time rather than
remembered — a workspace writes files, and that should be a decision.

**A workspace is refused where one could not exist.** A worktree needs a git
repository, and a conversation run somewhere that is not one cannot have a
branch of that kind. Discovering that when the branch is resumed — after it has
been created and named — is far too late, so `tab` says so and stays a thought.
For the same reason the flag is left out of a resume command whose directory
has stopped being a repository: a flag known to fail is worse than one absent.

`r` renames a conversation on the map, pre-filled with what it is called now.
Names live in braids' sidecar, so renaming never touches a transcript, and
emptying the field restores whatever the harness called it.

### 6.6 Subagent — in the conversation that spawned it

```
│ ●  t633 claude  Read                                                           │
│   ├─⊕ Explore · Verify console edit points                            42 turns │
│   ├─⊕ Explore · Verify job-watcher extension points                   49 turns │
│   ├─⊕ microagi:code-reviewer · Harsh pre-PR review of pipeline fix    31 turns │
```

A subagent is a whole conversation the harness collapses into one `tool_use` and
one `tool_result`. One real lane here hides **ten of them, 409 turns in total**,
none of which Claude Code offers any way to read.

They are drawn against the turn that spawned them, joined by the `toolUseId` in
each agent's meta file.

**`↵` reads one in place**, from its own transcript, writing nothing. Deciding
whether an agent is worth keeping should follow reading it, not precede it — and
promoting first would have meant a file on disk for every agent you merely
glanced at. While reading one, the actions that assume a resumable session — `b`,
`y`, `o` — say so rather than half-working: a subagent is not a session until it
is promoted.

**`p` promotes it**, from the parent's spine or from inside the agent being read:
clearing the sidechain mark and giving it a session ID is enough for the harness
to resume it. Verified end to end — a promoted agent answered a question about
its own earlier work.

A promoted agent shares no message IDs with its parent, so nothing could infer
where it came from. Its provenance is recorded (§4), which is what hangs it
under the conversation that spawned it.

### 6.7 Archive and delete

`a` archives the selected conversation and `A` reveals what is archived. That is
the gesture for most tidying: instant, reversible, and it leaves the
conversation searchable, so nothing has to be deleted to get a map that reflects
what is being worked on. An archived row keeps its place in the tree when shown,
drawn with `○` rather than `●`.

**The title always says what is being held back** — `Conversations(all)[31] · 4
archived hidden` — because a map that silently omits things is one you stop
trusting.

`d` deletes, moving everything a conversation owns — the transcript and the
directory beside it holding its subagents and tool output — into
`~/.braids/trash/`, and reporting what was reclaimed.

### 6.8 The bin — recovering something deleted days ago

```
 Deleted:    3                    <j/k>   down / up     <d>     delete for good
 Holding:    7 kB                 <↵ / r> restore       <esc>   back
 Kept for:   14 days
 Next to go: in 1h

╭─ Deleted[3] ───────────────────────────────────────────────────────────────────╮
│ CONVERSATION                              DELETED       SIZE       EXPIRES      │
│ deleted an hour ago                         1h ago     4.0 kB       in 13d      │
│ the one you want back                       2d ago     2.0 kB       in 12d      │
│ nearly expired                             13d ago     1.0 kB        in 1h      │
╰────────────────────────────────────────────────────────────────────────────────╯
```

`u` opens it. A one-step undo was the wrong shape: it reached one deletion back,
and only for as long as the session lived — no help at all for wanting the
eighth of ten conversations back two days later. Recovering something means
seeing what was deleted, which is a screen rather than a keystroke.

Each entry carries its own manifest beside the files it holds, so the bin is
readable by a session that did not do the deleting. `↵` restores, `d` removes
for good, and every row says how long is left before it goes on its own —
**turning amber inside the last two days**, so nothing quietly passes the point
of recovery. Retention is 14 days, and expiry runs when the bin is opened so the
deadlines shown are true.

Two rules the notice states outright:

- **It never cascades.** A fork carries its own copy of the prefix it shares, so
  deleting a parent cannot break a child. That is the fear that makes people
  hoard, and here it is simply not true.
- **Orphans move up, not out.** A branch whose parent was deleted is redrawn
  under the nearest conversation that is still there, following what was
  recorded (§4). Inference cannot do this on its own: every fork of a fork
  shares a byte-identical prefix with the whole line above it, so the counts tie
  and the branch lands at the shallowest ancestor. Where nothing was recorded —
  a branch made before braids, or by `/branch` — that tie is still the best
  available answer, and it is why provenance is recorded at all.
- **A running conversation is refused.** Deleting a session mid-turn is the one
  accident worth preventing outright; `d` from inside a conversation is
  redirected to the map, where you can see what would go.

**`D` discards a conversation's work products and keeps the conversation.** A
harness stores scratch files and job records outside the transcript, and they
dwarf it: **3.5 GB against 365 MB** here, held by two conversations out of
thirty-three. Letting them go costs nothing that can be read again, which makes
it the deletion nobody regrets — and it goes to the same bin, so even that is
reversible.

A `WORK` column appears when anything has them, blank where there are none so
the eye lands only on what is actually holding something. Sizing costs a walk of
a few thousand directory entries, which is milliseconds.

Still to come: multi-select and the filtered sweep.

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
| `n` `N` | next / previous marker | | *(spine)* |
| `1`–`9` | jump to lane | `?` | help |
| `q` | quit | `r` | rebuild index |

`esc` = go up a level, echoing Claude Code's own `esc esc` rewind.

---

## 8. Core interactions

**Branch from a message.** Walk the raw chain from the chosen turn to the root —
bookkeeping records included, since the harness wrote them — copy those records
into a new file with a fresh `sessionId`, prepend a `custom-title` so the branch
is named on the map, and append a `last-prompt` pinning the leaf. The new file is
built in a temp file and renamed into place, so an interrupted branch leaves
either a whole lane or none. **The source is opened read-only and never written
to**, which a test asserts by hashing it before and after.

Verified end to end: a branch cut at turn 2 of a real conversation resumed under
`claude --resume` with exactly that prefix as its context, and appeared on the
map as a child at `← t2`.

**Clone a graph.** Copy each lane's file, rewrite `sessionId`, keep message uuids.
Topology reassembles itself, because forks are detected by shared uuids.

**Promote a subagent.** Set `isSidechain: false`, rewrite `sessionId`, drop
`agentId`, write as a top-level file.

**Merge (later).** Splice a sibling's unique nodes onto the current leaf with
fresh uuids. Real messages, not a summary. Gated behind a preview.

**Continue a conversation.** `y` copies `claude --resume <id> --name <title>`
through the terminal's own copy escape (OSC 52), so it works over SSH with no
helper binary. `o` opens a terminal when `BRAIDS_SPAWN` names one — a template
understanding `{cmd} {id} {name} {dir}` — and otherwise copies the command and
says how to configure a launcher.

braids does not guess at a terminal. Terminals differ in whether they can be
told to run a command at all, and the working directory matters: resuming from
elsewhere files the transcript under a different project, so a lane carries the
`cwd` its conversation ran in.

**Delete.** Move files to trash. Never cascades. Never allowed on a running lane.

**Branch a running lane — allowed.** Forking writes a new file, so the running
session is untouched. Permitted from any *completed* turn; only the in-flight
turn is blocked, because its records are still being written. This is the point:
you watch an agent go wrong, branch from three turns before it did, and try
another way without killing it.

**Auto-naming today** is a plain stop-word filter over the turn's own words —
`"why is the queue stalling"` → `queue-stalling` — pre-filled into an editable
field. The df-banded version below is the intended upgrade once the index
exposes term frequencies; it is not what ships yet.

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
- **The cursor follows the conversation, and when it is gone, the row above
  it.** Archiving or deleting the selected row would otherwise throw the reader
  to the top of the list on every act of tidying, which is exactly when losing
  your place costs most.
- Never render more than one junction per row.
- **The header and the table are laid out by the same code.** Columns drop from
  the right as the terminal narrows — size, then status, then project, then
  turns — keeping the name and the age, which answer what it is and whether it
  is stale. The two width bugs this screen has had were both the header and the
  rows computing their own widths and drifting apart.
- **Every binding a screen has is in its legend.** A key that works but is not
  listed may as well not exist, so the header grows rows and drops the glyph key
  before it drops a key. Only when even one column will not hold them all does
  it keep what it can — ordered so moving, opening, searching and quitting
  survive.
- A glyph key sits beside the facts when there is room left after the keys. **Each mark in it is drawn in the
  style it is drawn in on the screen** — green for alive, the accent for what
  wants you, grey for the rest. Colour here is meaning, so a key rendered in one
  flat colour would teach the wrong thing.
- A paired key says what each half does: `n / N  next / prev marker`, not
  `n/N  next marker`.
- Animation is limited to a lane growing out of a junction on branch creation.
  That single motion is what teaches the model.

**Glyph width is a correctness issue.** `● ◆ ○ ← ⚠ ⊕` are East-Asian *Ambiguous*
width: some terminals and fonts render them double-wide, which silently breaks
every box in this document. Pick glyphs from the unambiguous-narrow set where
possible, measure with `go-runewidth` rather than `len()`, and add a
`--ascii` fallback (`* + o <- ! @`) for terminals that get it wrong. Golden-file
tests via `teatest` should assert frame width, not just content.

---

### 9.1 Other harnesses

v1 is Claude Code only. The primitive generalises to any agent that writes local
transcripts and can resume one by id, so `store.Source` is a port from day one —
but no second adapter ships until someone asks for it.

Verified on this machine:

| Harness | Storage | Shape | Fit |
|---|---|---|---|
| Claude Code | `~/.claude/projects/**/*.jsonl` | **DAG** — uuid/parentUuid | native |
| Codex | `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl` (493 MB here) | flat log, **no parent pointers** | works, file-level branches only |
| opencode | SQLite since v1.2 (was per-file JSON) | messages + parts | adapter reads SQLite |
| Aider | `.aider.chat.history.md` | markdown, linear, no resume-by-id | poor fit |

Two consequences for the domain model:

- **Branch points must not be assumed to live inside a transcript.** They do in
  Claude Code; in Codex a fork is only ever a separate file.
- **Prefix fingerprinting is the universal fork-detection method** — hash the
  opening run of turns and match across lanes. Shared-uuid matching (§5) is the
  exact fast path where a harness offers it, not the general algorithm.

Claude Code luxuries — subagent forest, `compactMetadata`, hook-driven
needs-you — are declared as optional `Capabilities`, never assumed.

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

1. ~~**Index + search.**~~ **Done.** Parse, FTS5, `braids search`.
2. ~~**The Map.**~~ **Done.** Lanes, fork detection by shared uuid, statuses
   from mtime, k9s-style chrome.
3. ~~**The Spine.**~~ **Done** for runs and junctions; compaction seams,
   subagent lanes and error marks still to come.
4. ~~**Branch.**~~ **Done**: file synthesis, naming, renaming, the `b` key,
   thought-versus-workspace, navigating into a branch from its parent, recorded
   provenance, and an immediate in-place refresh.
5. ~~**Live.**~~ **Done** for the watcher and in-place refresh: the map follows
   sessions as they are written, and a branch appears without any command.
   Hooks and the needs-you queue still to come. ← *next*
6. ~~**Search screen.**~~ **Done**: full text across every conversation, with
   `↵` jumping to the turn it found.
7. ~~**Lane state.**~~ **Done** from files: working, thinking, your turn,
   stopped, unanswered, with `n`/`N` stepping between what is owed. Hooks
   would add the running-versus-blocked distinction.
8. ~~**Subagents.**~~ **Done**: discovered, indexed against the turn that
   spawned them, drawn in the spine, and promotable with `p`.
9. ~~**Housekeeping.**~~ **Done**: archive, delete to a bin, and a bin you can
   browse and recover from. Multi-select, the filtered sweep and reclaiming job
   artifacts remain.
10. **Merge.** Splice with preview.

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

<div align="center">

<img src="assets/braids-logo.png" alt="braids" width="140">

# braids

**Branch your Claude Code conversations. Resume any of them from any turn.**

[![ci](https://github.com/Ashes47/braids/actions/workflows/ci.yml/badge.svg)](https://github.com/Ashes47/braids/actions/workflows/ci.yml)
[![go](https://img.shields.io/badge/go-1.25-00ADD8)](https://go.dev)
[![license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

</div>

---

Claude Code works best on one linear thread doing one thing. Humans don't work
that way — a task forks, doubles back, and spawns three side quests. Today the
only way to cope is to run N terminals and hold the map in your head.

**braids is that map.** One terminal window alongside your N others. It shows
every conversation and every branch as a graph, searches all of them in about a
millisecond, and turns any message into the start of a new branch.

It never talks to Claude. It arranges the conversations you have with Claude.

```
 Source:         claudecode                            <j/k>   down / up
 Index:          ~/.braids/index.db                    <↵>     open spine
 Lanes:          6                                     </>     search
 Waiting on you: 5                                     <f>     filter list
 Hooks:          reporting                             <a>     toggle archive
                                                       <r>     rename
                                                       <q>     quit
╭─ Conversations(all)[6] ──────────────────────────────────────────────────────────╮
│  CONVERSATION                                  TURNS      SIZE    AGE      STATUS│
│ ● try-option-c                                    75     16 kB    39m  unanswered│
│ ● nvidia-delivery                                824    178 kB    41m   your turn│
│ ● cache-gate-probe                                14      3 kB     3h   your turn│
│ ● gcsfuse-density                                125     27 kB     2d  unanswered│
│ ● annotation-pipeline                            368     79 kB     5d   your turn│
│ ● mdt-contention                                  41      9 kB    14d  unanswered│
│                                                                                  │
│                                                                                  │
│                                                                                  │
│                                                                                  │
│                                                                                  │
│                                                                                  │
╰──────────────────────────────────────────────────────────────────────────────────╯
 b2c3d4e5-0000-4000-8000-000000000002
```

## Why

A conversation with an agent is a single line of context. That is a good fit for
the model and a bad fit for the work: you want to try an idea without poisoning
the thread, come back to a decision made an hour ago and go the other way, or
run four attempts at once and keep the one that worked.

braids keeps the graph for you, and gives the model a clean linear path. Every
root-to-leaf route through the graph is one ordinary Claude Code session — no
wrapper, no proxy, no protocol of its own.

## Install

```sh
go install github.com/Ashes47/braids/cmd/braids@latest
```

Or from source:

```sh
git clone https://github.com/Ashes47/braids && cd braids
make install          # -> $(go env GOPATH)/bin/braids
```

Then:

```sh
braids index          # read ~/.claude/projects (once; a few seconds)
braids                # open the map
```

## What it does

**Map** — every conversation as a tree, with branches shown under the
conversation they were cut from, and how far behind each one is.

**Spine** — one conversation collapsed to its landmarks: what you asked, what
came back, where it failed, where it was compacted. A 25,000-turn conversation
becomes a few hundred lines you can actually read.

**Search** — full text over every message and tool call, in about a millisecond.
Search is the front door; the graph is the confirmation.

**Branch** — put the cursor on any turn and press `b`. braids writes a new
session file containing that turn's ancestry, and hands you the `claude --resume`
command. Add `--workspace` and the branch gets a git worktree of its own, so two
branches can edit the same file without touching each other.

**Merge** — join a branch back as a new conversation, splicing the real turns
from both. braids refuses when one side already contains the other, rather than
producing a duplicate wearing a new name.

**Promote** — a subagent's transcript is a conversation too. Turn one into a lane
of its own and carry on from where it left off.

**Work products** — a session's scratch usually dwarfs its transcript: 3.3 GB
against 363 MB here, with nine files holding 1.3 GB of it. `w` opens it as a
size browser — heaviest first, directories weighed by what is under them, `↵` to
descend — so you can bin one 231 MB dump and keep the rest. The harness's own
record of a job is shown and refused. `braids work --orphans` finds the sets
whose conversation is gone.

**Memories** — what a project has told the harness to remember, with the two
things you cannot see from inside a session: a memory the index omits, which is
therefore loaded by nothing, and a link pointing at a memory that is gone. `↵`
on a memory opens the conversation that wrote it. Read-only for now.

**Bin** — deleting moves files aside with a manifest and a 14-day retention, so
nothing you delete is gone the moment you regret it.

## Driving it from an agent

Every command that reports something takes `--json`, so braids is usable by the
thing it is watching — Claude Code can search its own past conversations, find
where a decision was made, and branch from that exact turn.

```sh
braids search "why did we drop the retry" --json | jq -r '.hits[0] | .lane, .turn'
braids branch --lane <id> --at <turn> --json | jq -r .resume
```

Two properties make this work, and both are deliberate:

- **IDs are whole in JSON.** The tables shorten them with an ellipsis to fit a
  terminal; `--json` never does. (Pasting a shortened one back works too — braids
  accepts the ellipsis rather than refusing an ID it printed itself.)
- **Empty is `[]`, not a sentence.** Nothing should have to tell "no matches"
  apart from a parse failure.

## Commands

```
braids                                  open the map
braids index [--full]                   index new and changed transcripts
braids search QUERY [--kind K] [--limit N]
braids lanes                            list indexed conversations
braids agents  --lane ID                list the subagents a conversation spawned
braids work    --lane ID [--path SUB]   browse a session's work products
braids work    --orphans [--reclaim]    find, and reclaim, ownerless ones
braids memories [--project NAME]        what a project remembers, and whether
                                        the index still agrees with the files
braids branch  --lane ID --at TURN [--workspace]
braids promote --lane ID --agent ID     turn a subagent into its own conversation
braids merge   --lane ID --from ID [--plan]
braids hooks [--install|--remove]       let sessions report when they block
braids version
```

Every command takes `--help` for its own flags and `--json` if it reports
something. A mistyped command names the one you meant.

## Hooks are optional

Files can tell you a session is mid-tool-call. They cannot tell you whether it is
*running* or *waiting for your approval* — both look identical on disk. One hook
can. `braids hooks --install` asks Claude Code to report it.

It is opt-in and separate from installing the binary: a tool that edits your
settings file as a side effect of being installed is not one to trust with it.
Installing merges rather than writes — every hook already there is kept, a
timestamped copy of the previous file is left beside it, `--remove` takes back
only what braids added, and a settings file that cannot be parsed is refused
rather than replaced by a guess.

Everything else works without them. `braids hooks` says which mode you are in,
and so does the map.

## Privacy

braids reads your transcripts, so this matters more than usual:

- **It makes no network calls.** There is no HTTP client and no listener in it.
  `go list -deps ./cmd/braids | grep net/http` comes back empty — check it
  yourself.
- **Nothing leaves your machine.** The index is a local SQLite file at
  `~/.braids/index.db`.
- **What it writes stays private to you.** `~/.braids` is `0700` and the index
  is `0600`, because it holds the full text of every message it has read and
  Claude Code keeps the transcripts that came from at `0700`. An index left
  loose by an older build is tightened the next time braids opens it.
- **It never writes to a transcript Claude Code owns.** Branching writes a new
  session file; the source is opened read-only. One writer per file, always.
- **Delete braids and you lose a view, never a conversation.** Everything is
  derived from `~/.claude/`.

## Numbers

Measured on one laptop against a real corpus — 28 conversations, ~62,000
messages, ~360 MB of transcripts. Rounded, because a corpus you are still
working in never sits still:

| | |
|---|---|
| First index, from nothing | ~8 s |
| Re-index, a few conversations changed | ~300 ms |
| Re-index, nothing changed | under 1 ms |
| Search, across ~62,000 indexed units | 1–6 ms |
| Index on disk | ~190 MB |

The last row is the honest cost: the index is roughly half the size of the
transcripts it was built from, because it stores the text again to search it.

## Principles

1. **Files are the truth.** Everything is derived from `~/.claude/`.
2. **One writer per file, always.** Every branch is its own session file.
3. **The graph is for humans; each path is linear for the model.**
4. **Search is the front door; the graph is the confirmation.**
5. **A window you glance at, not a place you live.**

## Non-goals

braids does not talk to any model, proxy your session, or replace your terminal.
It does not sync anything anywhere. It is one window next to the ones you already
have open.

## Security

`BRAIDS_SPAWN` is run through a shell, so every value braids substitutes into it
is shell-quoted first: a conversation's title and directory are data read out of
a transcript, and a conversation called `x; rm -rf ~ #` must be a name rather
than a command. Your template supplies the shell syntax; the values never do —
so do not put quotes around a placeholder yourself.

Found something? Open an issue for anything that is not exploitable, and email
the address in `git log` for anything that is.

## Other harnesses

Claude Code is the only source today. The seam is deliberate: a `Source` port
with optional capability interfaces, so a harness that cannot branch still gets
the map and search. See [SPEC.md](SPEC.md).

## Contributing

```sh
make ci     # fmt, vet, lint, test, race, cover — the same gate CI runs
```

Issues and pull requests welcome. [SPEC.md](SPEC.md) is the design doc; every
decision in it traces to something measured rather than assumed.

## License

MIT

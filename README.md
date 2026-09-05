<div align="center">

<img src="assets/braids-logo.png" alt="braids" width="140">

# braids

**Find the reasoning in your past Claude Code sessions, and carry on from it.**

[![ci](https://github.com/Ashes47/braids/actions/workflows/ci.yml/badge.svg)](https://github.com/Ashes47/braids/actions/workflows/ci.yml)
[![go](https://img.shields.io/badge/go-1.25-00ADD8)](https://go.dev)
[![license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

[**braids.chat**](https://braids.chat) &nbsp;·&nbsp; [**Docs**](https://braids.chat/docs/) &nbsp;·&nbsp; [**Install**](#install)

</div>

---

You have worked this out before. Somewhere in a session three weeks ago is the
turn where you and Claude figured out why the lock was held across a network
call, and neither of you can find it.

**braids finds it.** Every Claude Code session on your machine, searchable in a
few milliseconds: what you asked, what came back, what a tool returned, what a
project remembers. Then it turns that turn into the start of a new
conversation, so the reasoning gets reused rather than rediscovered.

It also draws every conversation and every branch as one graph, because once
you can find things you want to see what you have.

It never talks to Claude. It arranges the conversations you have with Claude.

<img src="assets/frames/map.png" alt="braids: the map, with six conversations and their branches" width="100%">

<sub>A real frame, colours and all. Every screenshot here is captured by
<code>scripts/demo.py</code> against a fake <code>~/.claude</code>, never drawn
by hand.</sub>


## Why

A conversation with an agent is a single line of context. That is a good fit for
the model and a bad fit for the work: you want to try an idea without poisoning
the thread, come back to a decision made an hour ago and go the other way, or
run four attempts at once and keep the one that worked.

braids keeps the graph for you, and gives the model a clean linear path. Every
root-to-leaf route through the graph is one ordinary Claude Code session: no
wrapper, no proxy, no protocol of its own.

## Install

```sh
curl -fsSL https://braids.chat/install.sh | sh
```

Run it again to update: it asks the binary it would replace what version it is,
says so and stops when there is nothing to do, and replaces braids where it
already lives rather than leaving a second copy elsewhere on your PATH.

That works out the release build for your machine, checks it against the
published checksums, and puts one binary on your PATH. Nothing else: no
daemon, no configuration file, and nothing to uninstall but the file.

Or with Go:

```sh
go install github.com/Ashes47/braids/cmd/braids@latest
```

On Windows, download the zip from
[releases](https://github.com/Ashes47/braids/releases) and put `braids.exe` on
your PATH. Everything reads and searches the same; opening a terminal with `o`
and updating with `v` are not offered there, because both would mean guessing at
shell quoting braids cannot test.

Or from source:

```sh
git clone https://github.com/Ashes47/braids && cd braids
make install          # -> $(go env GOPATH)/bin/braids
```

## First run

```sh
braids index          # read every transcript under ~/.claude (a few seconds)
braids                # open the map
```

Then, on the map:

| | |
|---|---|
| `j` `k` | move |
| `↵` | open a conversation's spine: its landmarks, not every line |
| `b` | branch from the turn under the cursor |
| `y` | copy the `claude --resume` command for it |
| `/` | search every conversation, memory and work product |
| `w` | browse what a session wrote to disk |
| `M` | what the project remembers |

Nothing here is a command you have to remember: the header lists every binding
the screen has, and a mistyped one names the key you meant.

Optionally, `braids hooks --install` asks Claude Code to report when a session
is blocked on you, which is the one thing the files cannot say. It is opt-in, merges
with whatever hooks you already have, and `--remove` takes back only what
braids added.

## What it does

**Map.** Every conversation as a tree, with branches shown under the
conversation they were cut from, and how far behind each one is.

<img src="assets/frames/spine.png" alt="braids: one conversation reduced to its landmarks" width="100%">

**Spine.** One conversation collapsed to its landmarks: what you asked, what
came back, where it failed, where it was compacted. A 25,000-turn conversation
becomes a few hundred lines you can actually read.

<img src="assets/frames/search.png" alt="braids: one search across conversations, memories and work products" width="100%">

**Search.** Full text over every message and tool call, plus your memories and
the names of every work product, in a few milliseconds. Each result says whether
it is a conversation, a memory or a work product, and `↵` opens the screen that
can act on it. Search is the front door; the graph is the confirmation.

**Branch.** Put the cursor on any turn and press `b`. braids writes a new
session file containing that turn's ancestry, and hands you the `claude --resume`
command. Add `--workspace` and the branch gets a git worktree of its own, so two
branches can edit the same file without touching each other.

**Merge.** Join a branch back as a new conversation, splicing the real turns
from both. braids refuses when one side already contains the other, rather than
producing a duplicate wearing a new name.

**Promote.** A subagent's transcript is a conversation too. Turn one into a lane
of its own and carry on from where it left off.

<img src="assets/frames/work.png" alt="braids: a session's work products, heaviest first" width="100%">

**Work products.** A session's scratch usually dwarfs its transcript: 3.3 GB
against 363 MB here, with nine files holding 1.3 GB of it. `w` opens it as a
size browser: heaviest first, directories weighed by what is under them, `↵` to
descend. So you can bin one 231 MB dump and keep the rest. `↵` on a file shows
its head, `y` copies its path, and a binary is named rather than spewed.

<img src="assets/frames/file.png" alt="braids: peeking at the head of a file a session wrote" width="100%"> The harness's own
record of a job is shown and refused. `braids work --orphans` finds the sets
whose conversation is gone.

<img src="assets/frames/memories.png" alt="braids: what a project remembers, with the memory nothing loads marked" width="100%">

**Memories.** What a project has told the harness to remember, with the two
things you cannot see from inside a session: a memory the index omits, which is
therefore loaded by nothing, and a link pointing at a memory that is gone. `↵`
reads it as markdown, `c` opens the conversation that wrote it.

<img src="assets/frames/memory.png" alt="braids: reading a memory with its markdown rendered" width="100%"> `d` deletes one
to the bin, `r` renames it and follows the name through every link, and `i`
repairs the index. Each of them changes the index in the same breath as the
file, and is refused while a session in that project is running.

<img src="assets/frames/bin.png" alt="braids: the bin, holding a deleted file for 14 days" width="100%">

**Explain.** `braids explain <file>` joins two things git and braids each half
know: git knows when a file changed, braids knows what was being said in that
directory at the time. For each commit it names the conversations that were live
in the window before it and quotes the last thing actually said. It does not
claim the conversation caused the commit, because it cannot know that: it offers
where to look, which is the honest thing to compute without reading meaning.

**Bin.** Deleting moves files aside with a manifest and a 14-day retention, so
nothing you delete is gone the moment you regret it.

## A skill for Claude

```sh
braids skill --install
```

That writes a Claude Code skill into `~/.claude/skills/braids`, teaching Claude
when to search your history, how to read the JSON, and how to branch from what
it finds. It fires when you refer to earlier work, ask why something is the way
it is, or propose something that may already have been tried, and not
otherwise. `braids skill` says whether the installed one is still current, since
an older braids leaves an older skill behind. A test checks every command and
flag in it against the program, so it cannot drift from what braids takes.

## Driving it from an agent

Every command that reports something takes `--json`, so braids is usable by the
thing it is watching. Claude Code can search its own past conversations, find
where a decision was made, and branch from that exact turn.

```sh
braids search "why did we drop the retry" --json | jq -r '.hits[0] | .lane, .turn'
braids branch --lane <id> --at <turn> --json | jq -r .resume
```

Two properties make this work, and both are deliberate:

- **IDs are whole in JSON.** The tables shorten them with an ellipsis to fit a
  terminal; `--json` never does. (Pasting a shortened one back works too, since braids
  accepts the ellipsis rather than refusing an ID it printed itself.)
- **Empty is `[]`, not a sentence.** Nothing should have to tell "no matches"
  apart from a parse failure.

## Commands

```
braids                                  open the map
braids index [--full]                   index new and changed transcripts
braids search QUERY [--type T] [--kind K] [--limit N]
braids lanes                            list indexed conversations
braids agents  --lane ID                list the subagents a conversation spawned
braids work    --lane ID [--path SUB]   browse a session's work products
braids work    --orphans [--reclaim]    find, and reclaim, ownerless ones
braids memories [--project NAME]        what a project remembers, and whether
                                        the index still agrees with the files
braids explain FILE                     which conversations were live when
                                        this file last changed
braids show    --lane ID [--at TURN]    read the turns around one turn
braids branch  --lane ID --at TURN [--workspace]
braids promote --lane ID --agent ID     turn a subagent into its own conversation
braids merge   --lane ID --from ID [--plan]
braids hooks [--install|--remove]       let sessions report when they block
braids skill [--install|--remove]      teach Claude Code to use braids
braids version
```

Every command takes `--help` for its own flags and `--json` if it reports
something. A mistyped command names the one you meant.

## Hooks are optional

Files can tell you a session is mid-tool-call. They cannot tell you whether it is
*running* or *waiting for your approval*. Both look identical on disk. One hook
can. `braids hooks --install` asks Claude Code to report it.

It is opt-in and separate from installing the binary: a tool that edits your
settings file as a side effect of being installed is not one to trust with it.
Installing merges rather than writes: every hook already there is kept, a
timestamped copy of the previous file is left beside it, `--remove` takes back
only what braids added, and a settings file that cannot be parsed is refused
rather than replaced by a guess.

Everything else works without them. `braids hooks` says which mode you are in,
and so does the map.

## Privacy

braids reads your transcripts, so this matters more than usual:

- **It makes no network calls.** There is no HTTP client and no listener in it.
  `go list -deps ./cmd/braids | grep net/http` comes back empty. Check it
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

Measured on one laptop against a real corpus of 28 conversations, ~62,000
messages, ~360 MB of transcripts. Rounded, because a corpus you are still
working in never sits still:

| | |
|---|---|
| First index, from nothing | ~8 s |
| Re-index, a turn added to a 145 MB conversation | ~40 ms |
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

## Uninstall

```sh
braids hooks --remove                     # if you installed them
rm -rf ~/.braids                          # the index, the bin, the sidecars
rm "$(go env GOPATH)/bin/braids"          # the binary
```

Your conversations are untouched by all three. They were never braids' to
begin with.

## Security

The threat model, the permissions braids sets and how to report something are
in [SECURITY.md](SECURITY.md). The one worth repeating here: `BRAIDS_SPAWN`
runs through a shell, so every value braids substitutes into it is shell-quoted
first, because a conversation called `x; rm -rf ~ #` has to be a name rather than a
command. Your template supplies the shell syntax; the values never do, so do
not put quotes around a placeholder yourself.

## Other harnesses

Claude Code is the only source today. The seam is deliberate: a `Source` port
with optional capability interfaces, so a harness that cannot branch still gets
the map and search. See [SPEC.md](SPEC.md).

## Contributing

```sh
make ci     # fmt, vet, lint, test, race, cover: the same gate CI runs
```

[CONTRIBUTING.md](CONTRIBUTING.md) has the layout, the decisions that look like
missing features, and what a good change looks like. [SPEC.md](SPEC.md) is the
design doc: nearly every decision in it traces to something measured on a real
machine rather than assumed, and the ones that went wrong went wrong because
they were assumed.

## License

MIT

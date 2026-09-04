---
name: braids
description: Search past Claude Code conversations, find where a decision was made, and branch a new conversation from that exact turn. Use when the user refers to earlier work, asks why something is the way it is, or when you are about to propose something that may already have been tried.
---

# braids

braids indexes every Claude Code conversation on this machine and answers
questions about them from the command line. It never talks to a model and makes
no network calls; everything it reports is read from files the harness already
wrote.

Check it is there before relying on it:

```sh
braids version
```

If that fails, braids is not installed and nothing below applies. Say so rather
than guessing at history.

## When to reach for it

Three situations, and not otherwise. Searching history on every question is
slow and usually beside the point.

1. **The user refers to earlier work.** "like we did last week", "the session
   where we fixed the lock", "what did we decide about the cache".
2. **The user asks why something is the way it is**, and the answer is not in
   the code or its comments.
3. **You are about to propose an approach that may already have been tried.**
   One search is cheaper than repeating a week someone already spent.

## Finding something

```sh
braids search "lock held across a network call" --json --limit 5
```

Every reporting command takes `--json`. Narrow it when you can:

```sh
braids search deploy --project braids --since 30d --json
braids search "timed out" --kind tool_result --json
braids search "rate limit" --type memory --json
```

`--type` is `conversation`, `memory` or `artifact`; searching covers all three
unless narrowed, and work products are matched by name rather than contents.
`--since` and `--until` take a date (`2026-08-01`) or an age (`30d`, `6w`,
`12h`).

A hit carries the whole `lane` id and the `turn` it was found at. Use them:

```sh
braids branch --lane <lane> --at <turn> --name "narrow the lock" --json
```

That writes a new conversation holding everything up to that turn and prints
the `claude --resume` command for it. The transcript it branched from is opened
read only.

## Where a file came from

```sh
braids explain internal/core/index/index.go --json
```

This joins what git knows (when a file changed) with what braids knows (what
was being said in that directory at the time). For each commit it names the
conversations that were live in the window before it and the last thing
actually said in them.

**It does not know that those conversations caused those commits, and neither
do you.** Report it as where to look, never as why the code is the way it is.

## Other things it answers

```sh
braids lanes --json                    # every conversation, with resume commands
braids agents --lane <lane> --json     # subagents a conversation spawned
braids memories --json                 # what a project remembers, and what it has lost
braids work --lane <lane> --json       # what a session wrote to disk
braids hooks --json                    # whether waiting states are trustworthy
```

## Rules

- **Quote evidence, never assert history.** Say "the conversation on 21 August
  says X, at turn 1842" rather than "the project decided X". braids reports
  what was said; it does not know what was concluded.
- **Do not run `braids index` speculatively.** The map keeps the index current
  by itself. If a read says there is no index, tell the user to run it once.
- **IDs come back whole in JSON on purpose.** Pass them through unchanged; do
  not shorten them for display and then try to reuse them.
- **An empty result is `[]`, not an error.** Nothing found means nothing found.
- **Errors go to stderr and exit 1.** A non-zero exit is a real failure, not an
  empty answer.
- **One search, then stop.** If a search finds nothing, say so and move on
  rather than trying six phrasings.

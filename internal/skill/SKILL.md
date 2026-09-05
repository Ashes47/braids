---
name: braids
description: Search past Claude Code conversations, read the turns around what you find, and branch a new conversation from that exact point. Use when the user refers to earlier work, asks why something is the way it is, or when you are about to propose something that may already have been tried.
---

# braids

braids indexes every Claude Code conversation on this machine and answers
questions about them from the command line. It never talks to a model and makes
no network calls: everything it reports is read out of files the harness
already wrote, through a local index. A search takes milliseconds.

Check it is there before relying on it:

```sh
braids version
```

If that fails, braids is not installed and nothing below applies. Say so rather
than guessing at history.

## When to reach for it

Three situations.

1. **The user refers to earlier work.** "like we did last week", "the session
   where we fixed the lock", "what did we decide about the cache".
2. **The user asks why something is the way it is**, and the answer is not in
   the code or its comments.
3. **You are about to propose an approach that may already have been tried.**
   One search is cheaper than repeating a week somebody already spent.

## When not to

Most of the time. History is not context to gather by reflex. A search that was
never going to find anything still costs the user a pause and fills your
context with output you will not use.

Do not search when the code in front of you answers the question:

- "add a function that parses this payload"
- "fix this typo", "rename this variable", "run the tests"
- "what does this regex do"

Do not search for general knowledge. How a language feature works, what a
library does, what an error means in the abstract: braids knows what was said
on this machine and nothing else.

Do not search when the user has already told you the answer. If they say "we
use exponential backoff here", that is the answer. Going to look it up again is
not diligence.

## The shape of a lookup

Three steps, and the middle one is the one that is easy to skip.

```sh
braids search "lock held across a network call" --json --limit 5
braids show --lane LANE --at TURN --kind text --json
```

A hit names a conversation, the turn it was found at, and about twelve words of
snippet. **The snippet is a pointer, not the evidence.** Read the turns around
it before saying anything about what was decided: the sentence that matters is
usually the one after the one that matched, and a snippet is far too little to
tell a proposal from a conclusion.

Then, if the user wants to carry on from there:

```sh
braids branch --lane LANE --at TURN --name "narrow the lock" --json
```

That writes a new conversation holding everything up to that turn and prints
the `claude --resume` command for it. The transcript it branched from is opened
read only.

## Searching

This is full-text search over what was written. It matches words, not meanings,
so search the words that would literally be in the transcript.

- **Use the user's own words.** If they said "the payment retry thing", search
  `payment retry`, not `resilience strategy`.
- **Use identifiers.** File names, error text, function names, flags. These are
  the best queries braids takes, because they appear verbatim in tool calls and
  nowhere else.
- **A synonym is a different query.** `retry` will not find a conversation that
  only ever said `backoff`. If a search finds nothing, try the other word once,
  then stop.

Narrow when you can:

```sh
braids search deploy --project braids --since 30d --json
braids search "timed out" --kind tool_result --json
braids search "rate limit" --type memory --json
```

`--type` is `conversation`, `memory` or `artifact`; searching covers all three
unless narrowed, and work products are matched by name rather than contents.
`--since` and `--until` take a date (`2026-08-01`) or an age (`30d`, `6w`,
`12h`).

## Reading what you found

```sh
braids show --lane LANE --at 22403 --around 6 --kind text --json
braids show --lane LANE --from 100 --to 140 --plain --json
```

`--at` with `--around` reads a window either side of one turn, which is what a
search hit or an explanation leaves you holding. `--from` and `--to` read a
range. With neither it reads the end of the conversation, which is the answer
to "how did that one go".

`--kind text` drops tool calls and their results, which are two thirds of any
real conversation; leave it off when what you want *is* what a command
returned. Blocks are cut at `--chars` characters and say how much was removed,
so a tool result carrying a whole file does not arrive whole.

`--plain` takes out the terminal colour codes. A program that found a terminal
on the other end wrote them, and braids stores what was written, so by default
they are there: a test run that printed `1 failed` in red is stored with eleven
escape bytes inside that phrase. That matters twice over. They are bytes you
cannot read, and they sit inside the words, so searching the output you got
back for `1 failed | 64 passed` will not find it. **Pass `--plain` when you
want to read what a command said.** Leave it off when the codes are the point,
which is when the question is about the terminal output itself. It is applied
before `--chars`, so asking for plain text also buys you more of the words.

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

A conversation qualifies one of two ways, and `matched` in the JSON says which.
A session whose working directory was inside the repository is placed by that
alone. A session that ran above it, at the root of a workspace holding several
checkouts, is placed only if it named the file, and `names_the_file` counts how
often. The second is weaker: say so when you report it.

## Acting on what you find

Finding it is not the job. What you do next is.

- **If it was already tried, say so before you write anything.** Name what
  happened and let the user choose: "this was tried on 26 August and abandoned
  because the budget defaults made warm pools inert — reuse that approach, or
  take the newer one?" The same finding delivered afterwards, as a footnote
  under code you have already written, wasted the search.
- **If history disagrees with what the user wants now, quote it once and do
  what they asked.** They may know something the transcript does not. A
  conversation from three weeks ago does not overrule the person in front of
  you.
- **Say when you looked and found nothing.** "Nothing in your history mentions
  a retry policy for this" is a real answer, and it is worth a line. Silence is
  indistinguishable from never having checked.

## Other things it answers

```sh
braids lanes --json                    # every conversation, with resume commands
braids agents --lane LANE --json       # subagents a conversation spawned
braids memories --json                 # what a project remembers, and what it has lost
braids work --lane LANE --json         # what a session wrote to disk
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
- **One search, then read.** If a search finds nothing, try one other wording
  and stop. Six phrasings of the same query is not research.

---
name: braids
description: Search past Claude Code conversations, read the turns around what you find, and branch a new conversation from that exact point. Use when the user refers to earlier work, asks why something is the way it is, or when you are about to propose something that may already have been tried.
---

# braids

braids indexes every Claude Code conversation on this machine and answers
questions about them from the command line. It never talks to a model and makes
no network calls: everything it reports is read out of files the harness
already wrote, through a local index. A search takes milliseconds.

Once, the first time you are going to use braids in a session:

```sh
braids doctor --json
```

If that fails to run at all, braids is not installed and nothing below
applies. Say so rather than guessing at history.

It answers the only question worth asking before the first search, which is
whether what braids tells you can be believed. Every check carries `ok` and,
when it is false, a `fix` naming the command that settles it. **Run the fix
for the index if it names one.** Nothing keeps the index current while you
work: the map does it when somebody opens it, and nobody has it open during
your session, so a search can otherwise answer `0 hits` about a conversation
that happened this morning, which reads exactly like "this never happened".

The other checks are worth reading and rarely worth acting on unattended. A
missing hook means waiting states cannot be trusted; a stale skill means these
instructions are older than the braids running them. Say so and let the user
decide.

Once per session, not once per search.

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

Do not search for this conversation. "Earlier" often means earlier today, in
the session you are already in, and you have that: it is your context. braids
is for the sessions you were not in, or were in and have forgotten.

## Arriving somewhere

Before any of that, when you are starting work in a project and want to know
what state it is in:

```sh
braids brief --json
```

The conversations touched here recently, how many are owed a reply, one line
of what each was last saying, and what the project remembers. It is bounded:
the most recent few and a clipped line each, never a transcript. It scopes to
the directory you are in, so it answers "what is going on here" rather than
"what is going on".

This is worth running once when the user opens a subject rather than on every
question, and it is not a substitute for searching. It says what is recent,
not what is relevant.

## The shape of a lookup

Three steps, and the middle one is the one that is easy to skip.

```sh
braids search "lock held across a network call" --json --limit 5
braids show --lane LANE --at TURN --kind text --json
```

A hit's `of` says what was found: a `conversation`, a `memory` or an
`artifact`. A conversation hit names the conversation, the turn it was found
at, and about twelve words of snippet. **The snippet is a pointer, not the
evidence.** Read the turns around it before saying anything about what was
decided: the sentence that matters is usually the one after the one that
matched, and a snippet is far too little to tell a proposal from a conclusion.

The other two are documents rather than turns, and are read a different way.
See below.

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
### Cover the synonyms in one query

Words are matched and not meanings, so `retry` does not find a conversation
that only ever said `backoff`. Put the alternatives in one query rather than
searching twice:

```sh
braids search "retry OR backoff OR \"exponential backoff\"" --limit 200 --json
```

**Raise the limit when you do.** The limit is shared across the whole query, so
a rare word gets crowded out by a common one: on a real history, five
alternatives found fifteen conversations at `--limit 500` and seven at the
default of twenty. More alternatives need more room.

The whole of SQLite's FTS5 syntax works here:

| | |
|---|---|
| `a b` | **both**, in any order |
| `a OR b` | either |
| `a NOT b` | a, excluding anything that also has b |
| `"a b"` | that phrase, in that order |
| `a*` | prefix, so `deploy*` matches deployed and deployment |
| `NEAR(a b, 10)` | both, within ten words of each other |

**Bare words are AND.** `payment retry` requires both, which is narrower than
either word alone, and is the most common reason a search comes back empty
about a conversation that is really there. Two words is a narrowing, not a
widening. `OR` is the widening.

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

### A hit that is not a conversation

A `memory` or `artifact` hit has `turn: 0`, because a document has no turns and
`show` has nothing to open. Its `lane` is where it came from, not where to look
for it. Read these by their path.

```sh
braids memories --project NAME --json
```

Every memory comes back with its `path`, its `description`, the `links` to
other memories and `origin`, the conversation it was written in. The body is an
ordinary file: read it.

**A memory is the strongest thing braids holds.** Everything else it reports is
something that was said near something else; a memory is somebody writing down
what they concluded, in their own words, on purpose. When one answers the
question, quote it and say it is a memory. Then follow `origin` if the
conversation behind it matters.

```sh
braids work --lane LANE --json
```

An artifact hit is a file a session left on disk, and it is matched by name and
never by contents, so a hit means the name matched and nothing more. `entries`
carries the path of each one. Read the file if you want to know what is in it.

## Where a file came from

```sh
braids explain internal/core/index/index.go --json
```

This joins what git knows (when a file changed) with what braids knows (what
was being said in that directory at the time). For each commit it names the
conversations that were live in the window before it and the last thing
actually said in them.

`--window` is how long before a commit a turn still counts as context, three
hours by default. Widen it when the work was spread over a day and narrow it
when a repository is busy.

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
- **What silence means depends on why you looked.** Widen the query once, as
  above, then stop either way, but do not stop the same way. If the user
  pointed at earlier work,
  say you looked and found nothing: they may have the wrong project in mind, or
  have said it somewhere braids cannot see, and either way they need to know
  the search happened. If you were only checking before proposing something,
  carry on and say nothing. An absence of history is not a finding, and
  announcing one on every question turns a useful check into noise.
- **When the code and the history disagree, say so.** This is the most valuable
  thing braids can hand you and it is easy to walk past: the implementation in
  front of you does X, and a session three weeks ago rejected X for a reason.
  Do not assume the old decision still holds, because code changes for reasons
  that never reach a transcript. Do not let it pass either. Put both in front
  of the user and let them say which is out of date.

## Carrying work across conversations

Branching is one of three, and the skill used to teach only that one. All three
write a new conversation and leave what they came from untouched, and all three
print the `claude --resume` command for it. **Give that command to the user.**
braids does not resume anything and neither should you: a new conversation is
theirs to open, in their terminal, when they want it.

```sh
braids merge --lane LANE --from BRANCH --name "the lock, narrowed" --json
braids promote --lane LANE --agent AGENT --json
```

`merge` brings a branch back, as a new conversation holding the base and then
the branch's turns spliced on. Use it when a branch worked and the work should
continue on the main thread.

**`--plan` first, and read the failures.** It reports what would come over and
stops. `incoming_failed_turns` counts the branch's turns whose tool call came
back an error and `incoming_last_failed_turn` says where the last one was, so
a branch whose final act was a failing test can be seen before it is joined
rather than after. Near the end means unfinished; early and then quiet means
recovered from. Say which it is.

`promote` turns a subagent into a conversation of its own. It takes no name:
the subagent already has the task it was given, and that is what the new
conversation is called. `braids agents --lane LANE` lists them; a subagent that did substantial work is otherwise
reachable only through the conversation that spawned it, and promoting it gives
it a thread the user can resume directly.

## Other things it answers

```sh
braids lanes --json                    # every conversation, with resume commands
braids agents --lane LANE --json       # subagents a conversation spawned
braids memories --json                 # what a project remembers, and what it has lost
braids work --lane LANE --json         # what a session wrote to disk
braids hooks --json                    # the hook alone, in more detail than doctor gives
```

## Rules

- **Quote evidence, never assert history.** Say "the conversation on 21 August
  says X, at turn 1842" rather than "the project decided X". braids reports
  what was said; it does not know what was concluded.
- **Run `braids doctor --json` once a session, before the first search**, and
  act on the index fix if it names one. See the top of this file: nothing else
  keeps the index current while you work.
- **IDs come back whole in JSON on purpose.** Pass them through unchanged; do
  not shorten them for display and then try to reuse them.
- **An empty result is `[]`, not an error.** Nothing found means nothing found.
- **Errors go to stderr and exit 1.** A non-zero exit is a real failure, not an
  empty answer.
- **One search, then read.** Put the alternative wordings in that one search
  with `OR` rather than running it again and again. Six separate phrasings of
  the same query is not research, and it is not how this is done here.

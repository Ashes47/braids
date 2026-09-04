# Contributing

Thanks for looking. braids is small and opinionated, and the fastest way to get
a change merged is to know which of those two you are up against.

## Getting set up

```sh
git clone https://github.com/Ashes47/braids && cd braids
make ci        # fmt, vet, lint, test, race, cover: the same gate CI runs
make run       # build and open the map against your own ~/.claude
```

`make ci` runs the linter through `go run` at a pinned version, so there is
nothing to install first and your result is the one CI will get. If `make ci`
is green, your push is green.

## What braids will and will not do

Some of these look like missing features and are decisions. Please read them
before proposing one of them, and please do argue if you think a decision is
wrong, with a case rather than a patch.

- **braids never talks to a model.** It arranges the conversations you have
  with a harness. No proxying, no wrapping, no protocol of its own.
- **braids makes no network calls.** There is no HTTP client in it, and a
  change that adds one needs to justify itself against that being checkable in
  one command: `go list -deps ./cmd/braids | grep net/http`.
- **Files are the truth.** Everything braids shows is derived from what the
  harness already wrote. Delete braids and you lose a view, never a
  conversation.
- **One writer per file, always.** braids writes its own files. Where it edits
  a harness file, the memory index, it does so atomically, keeps the mode it
  found, and refuses while a session in that project is running.
- **Deletion is recoverable.** Everything braids deletes goes to a bin with a
  manifest and a retention window.

## What a good change looks like

**Measure before you decide.** Nearly every design choice in
[SPEC.md](SPEC.md) traces to something counted on a real machine rather than
assumed, and the ones that went wrong went wrong because they were assumed. If
you are changing behaviour that depends on scale, say what you measured.

**Tests describe the failure, not the code.** A test named
`TestAppendingMatchesReadingWhole` that grows a transcript a turn at a time and
compares every row against reading the file whole is worth more than five that
assert a getter returns what was set. Several real bugs in braids were found by
tests written that way, before anyone ran the program.

**Comments say why, never what.** The code says what.

**One reason per commit**, with a message that says what was wrong and what the
change does about it. Long is fine; vague is not.

## Layout

```
cmd/braids            the command line, and the wiring of everything below it
internal/core/store   the port a harness plugs into, and the Claude Code adapter
internal/core/index   SQLite and FTS5: the searchable copy of everything
internal/core/graph   forests, fork detection, and collapsing a lane to a spine
internal/core/memory  reading and curating what a project remembers
internal/core/…       artifacts, hooks, trash, sidecar, watch
internal/tui          the screens
internal/brand        the ASCII mark, so the CLI and the TUI agree on it
scripts/              the tooling: the demo corpus, the screenshots, the PNGs
site/                 braids.chat and its docs, generated from the frames
```

`internal/core` knows nothing about the terminal, and nothing in `core` imports
`internal/tui`. A harness other than Claude Code plugs in at `store.Source`
with optional capability interfaces, so one that cannot branch still gets the
map and search.

## Screenshots and the site

Every screenshot in the README and on braids.chat is real braids output. None
of it is drawn by hand, because a hand-drawn frame drifts from the program the
moment either changes, and a box whose borders do not line up is the first
thing a reader notices.

```sh
make frames   # rebuild the captures and the PNGs (needs the binary)
make pages    # regenerate the landing page and the docs from those captures
make site     # both, then serve it on http://localhost:8787
```

`scripts/demo.py` writes a fake `~/.claude` under `/tmp`, indexes it, and
captures each screen at 195 columns, which is the width where the header draws
the facts, the glyph key, every binding and the full mark. It never reads your
own transcripts. Frames land in `site/frames` as `.ans`, which keeps braids'
colours, and `.txt`, which is the same frame with the escapes removed so a diff
is readable.

The map, a spine and search come from `braids --print`. The other screens are
reached with keys, and braids does not have a flag for them, so
`scripts/shots` presses the keys instead: it drives the same router and the
same `View` the running program does. It is a tool, not part of the binary.

`scripts/ansi2png.py` redraws a capture as a PNG for the README, because
GitHub strips `style` attributes and will not render either ANSI or CSS.

Nothing generated is committed twice: `site/index.html`, `site/docs/` and
`site/assets/` are all built, and the Pages workflow builds them before it
deploys.

## Adding a harness

Implement `store.Source`, plus whichever of `Enricher`, `Sidechains`,
`Brancher`, `Promoter`, `Merger`, `Tailer`, `Measurer` and `Rememberer` you can
honestly support. braids hides what a source cannot do rather than showing a
key that fails. Start by reading `internal/core/store/claudecode`, where every
awkward thing in it is commented with the observation that forced it.

## Reporting a bug

The most useful bug report says what you did, what happened, and what you
expected. `braids version` prints the commit it was built from. If the map or a
screen is involved, `braids map --print --width 100` writes one frame to stdout,
which pastes into an issue without a screenshot.

Security issues: see [SECURITY.md](SECURITY.md).

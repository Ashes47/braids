# Security

braids reads every transcript under `~/.claude`, so it is worth being precise
about what it does with them.

## What braids does with your data

- **It makes no network calls.** There is no HTTP client and no listener in it.
  Check it yourself: `go list -deps ./cmd/braids | grep net/http` comes back
  empty.
- **Nothing leaves your machine.** The index is a local SQLite file at
  `~/.braids/index.db`.
- **What it writes stays private to you.** `~/.braids` is `0700` and the index
  is `0600`, because it holds the full text of every message it has read and
  the harness keeps the transcripts that came from at `0700`. An index left
  loose by an older build is tightened the next time braids opens it.
- **It never writes to a transcript the harness owns.** Branching writes a new
  session file; the source is opened read-only, and there is a test that hashes
  it before and after.
- **The one harness file it edits** is a project's memory index, when you ask
  it to. That is written atomically, keeps the mode it found, and is refused
  while a session in that project is running.

## Reporting a vulnerability

Open a GitHub issue for anything that is not exploitable: a wrong permission,
a path that is not validated, a claim above that turns out to be false.

For anything exploitable, email the address on the commits in `git log` rather
than opening an issue, and give it a few days before disclosing.

Please include what an attacker would need to control. braids' interesting
inputs are all local: a transcript, a memory file, a work product, a settings
file, and the `BRAIDS_SPAWN` template. "An attacker who can already write
arbitrary files to your home directory" is a threat model braids cannot defend
against and does not claim to.

## Things worth knowing

**`BRAIDS_SPAWN` runs through a shell.** That is the point of it, since a template
is only useful if it can use shell syntax. Every value braids substitutes into
it is shell-quoted first, because a conversation's title and directory are data
read out of a transcript and a conversation called `x; rm -rf ~ #` must be a
name rather than a command. Your template supplies the shell syntax; the values
never do, so do not put quotes around a placeholder yourself.

**Hooks are opt-in and separate from installing the binary.** `braids hooks
--install` edits `~/.claude/settings.json`, merging rather than replacing:
every hook already there is kept, a timestamped copy of the previous file is
left beside it, `--remove` takes back only what braids added, and a settings
file that cannot be parsed is refused rather than replaced by a guess.

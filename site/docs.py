#!/usr/bin/env python3
"""Build the braids docs.

One page per thing you might be trying to do, each with a sidebar listing all
of them and its own headings. The chrome comes from chrome.py, so the docs
cannot drift away from the landing page.

Everything here is checked against the program rather than remembered: the key
tables are the bindings each screen actually lists, the flag tables are what
`braids <command> --help` prints, and the JSON is real output with the paths
shortened.

    python3 site/docs.py
"""

import html
import pathlib
import re

from chrome import CSS, footer, frame, nav, page

HERE = pathlib.Path(__file__).parent
OUT = HERE / "docs"
HOME = "/"

DOCS_CSS = CSS + """
/* ---- docs layout ----
   Wider than the landing page, because a frame is 195 columns and the sidebar
   takes 224 of the window before the article gets any. */
.wrap.wide { max-width:1440px; }
.docs { display:grid; grid-template-columns:224px minmax(0,1fr); gap:48px; padding:38px 0 72px; }

.side { position:sticky; top:78px; align-self:start; max-height:calc(100vh - 100px); overflow-y:auto; }
.side h6 {
  margin:0 0 10px; font-family:var(--mono); font-size:11px; font-weight:500;
  letter-spacing:.09em; text-transform:uppercase; color:var(--faint);
}
.side nav { position:static; background:none; border:none; backdrop-filter:none; }
.side .group { margin-bottom:24px; }

/* Two lists doing two jobs, so they are drawn two ways. Which document you
   are in is a choice among ten, and it gets a solid bar. Where you are inside
   that document is a position along it, so the headings hang off a rail with
   a dot on it. Same accent, different shapes: nothing has to be read twice to
   work out which question it answers. */
.side a.page {
  display:block; position:relative; color:var(--dim); font-size:14.5px;
  padding:5px 0 5px 14px; border:0;
}
.side a.page:hover { color:var(--ink); text-decoration:none; }
.side a.page.on { color:var(--ink); font-weight:600; }
.side a.page.on::before {
  content:""; position:absolute; left:0; top:7px; bottom:7px;
  width:3px; border-radius:2px; background:var(--accent);
}

.side .onthis { margin:4px 0 10px 14px; border-left:1px solid var(--line); }
.side .onthis a {
  display:block; position:relative; font-size:13px; color:var(--faint);
  padding:4px 0 4px 16px; border:0;
}
.side .onthis a:hover { color:var(--ink); text-decoration:none; }
.side .onthis a.here { color:var(--accent); }
.side .onthis a.here::before {
  content:""; position:absolute; left:-4px; top:50%; margin-top:-3.5px;
  width:7px; height:7px; border-radius:50%; background:var(--accent);
}

/* ---- the docs index ---- */
.cards { display:grid; grid-template-columns:repeat(auto-fit,minmax(240px,1fr)); gap:18px; margin:26px 0 0; }
.cards a {
  border:1px solid var(--line); border-radius:11px; padding:16px 18px;
  background:var(--panel); color:var(--ink);
}
.cards a:hover { border-color:var(--faint); text-decoration:none; }
.cards a b { display:block; font-size:15.5px; margin-bottom:5px; }
.cards a span { color:var(--dim); font-size:14px; }

article h1 { font-size:clamp(26px,3.4vw,36px); margin:0 0 8px; letter-spacing:-.02em; }
article > p.lede { font-size:17px; margin:0 0 6px; }
article h2 {
  font-size:22px; margin:44px 0 8px; padding-top:18px; border-top:1px solid var(--line);
  scroll-margin-top:78px;
}
article h2:first-of-type { border-top:none; padding-top:0; margin-top:34px; }
article h3 { font-size:16.5px; margin:26px 0 4px; }
article p { margin:10px 0; }
article ul, article ol { max-width:72ch; color:var(--dim); padding-left:22px; }
article li { margin:5px 0; }
article li strong, article ol strong { color:var(--ink); font-weight:600; }
article table td:first-child { color:var(--accent); }
article table.keys td:first-child { white-space:nowrap; }
article .say {
  border-left:2px solid var(--accent); background:var(--panel); border-radius:0 8px 8px 0;
  padding:12px 16px; margin:20px 0; color:var(--dim); font-size:14.5px;
}
article .say strong { color:var(--ink); }
article .say p { margin:0; max-width:none; }
.pager { display:flex; gap:14px; margin-top:52px; padding-top:22px; border-top:1px solid var(--line); }
.pager a { flex:1; border:1px solid var(--line); border-radius:10px; padding:13px 16px;
           background:var(--panel); color:var(--ink); }
.pager a:hover { border-color:var(--faint); text-decoration:none; }
.pager a span { display:block; font-family:var(--mono); font-size:11px; color:var(--faint);
                letter-spacing:.08em; text-transform:uppercase; margin-bottom:3px; }
.pager a.next { text-align:right; }
@media (max-width:820px) {
  .docs { grid-template-columns:minmax(0,1fr); gap:22px; }
  .side { position:static; max-height:none; }
  .side .onthis { display:none; }
}
"""


# ---------------------------------------------------------------- content helpers

def sh(text: str) -> str:
    """A shell block. Lines starting with # are dimmed as comments."""
    out = []
    for line in text.strip("\n").split("\n"):
        stripped = line.lstrip()
        if stripped.startswith("#"):
            pad = line[: len(line) - len(stripped)]
            out.append(f'{pad}<span class="c">{html.escape(stripped)}</span>')
        elif "  #" in line:
            code, _, comment = line.partition("  #")
            out.append(f'{html.escape(code)}  <span class="c">#{html.escape(comment)}</span>')
        else:
            out.append(html.escape(line))
    return '<pre class="sh">' + "\n".join(out) + "</pre>"


def table(head: list[str], rows: list[tuple], cls: str = "") -> str:
    th = "".join(f"<th>{h}</th>" for h in head)
    body = ""
    for row in rows:
        body += "<tr>" + "".join(f"<td>{c}</td>" for c in row) + "</tr>"
    attr = f' class="{cls}"' if cls else ""
    return f"<table{attr}><thead><tr>{th}</tr></thead><tbody>{body}</tbody></table>"


def keys(rows: list[tuple]) -> str:
    return table(["Key", "What it does"],
                 [(f"<kbd>{k}</kbd>", a) for k, a in rows], cls="keys")


def say(text: str) -> str:
    return f'<div class="say"><p>{text}</p></div>'


def code(text: str) -> str:
    return f"<code>{html.escape(text)}</code>"


# ---------------------------------------------------------------------- pages

START = f"""
<h1>Start here</h1>
<p class="lede">
  braids reads the transcripts Claude Code already writes and draws them as a
  graph you can search, branch and prune. It never talks to a model.
</p>

<h2 id="what">What it is</h2>
<p>
  Claude Code keeps one conversation per session, in one JSONL file under
  <code>~/.claude/projects</code>. That works while a session stays on one
  thread. Real work does not: you fork to try something, double back, spawn
  three side quests, and a week later you have thirty session files with no
  shape between them.
</p>
<p>braids gives them a shape. It does four things.</p>
<ol>
  <li><strong>Draws the graph.</strong> Every conversation, with branches under
    the conversation they were cut from.</li>
  <li><strong>Searches all of it.</strong> Messages, tool calls, project
    memories, and the names of files a session left on disk.</li>
  <li><strong>Branches anywhere.</strong> Put the cursor on a turn, get a new
    conversation that starts there.</li>
  <li><strong>Shows the mess.</strong> Scratch files, memories the index has
    lost track of, subagents you never saw.</li>
</ol>
{say("braids does not call a model, and it does not need an API key. It arranges the conversations you have with one. See <a href='/docs/privacy/'>Files and privacy</a> for how to check that claim yourself.")}

<h2 id="install">Install</h2>
{sh('''
# picks the build for your machine and checks it against the published sums
curl -fsSL https://braids.chat/install.sh | sh

# or with Go
go install github.com/Ashes47/braids/cmd/braids@latest

# or from source
git clone https://github.com/Ashes47/braids && cd braids
make install
''')}
<p>
  The installer puts one binary in <code>/usr/local/bin</code> when that is
  writable, and <code>~/.local/bin</code> otherwise. Set
  <code>BRAIDS_BIN_DIR</code> to choose, or <code>BRAIDS_VERSION</code> to pin
  a version.
</p>

<h3>Uninstalling</h3>
{sh('''
braids hooks --remove    # take the hook back out of ~/.claude/settings.json
rm "$(command -v braids)"
rm -rf ~/.braids         # the index and its sidecar files
''')}
<p>
  Your conversations live in <code>~/.claude</code> and are never touched, so
  removing braids costs you the map and nothing else.
</p>

<h3>Windows</h3>
<p>
  There are Windows builds, as a zip on the
  <a href="https://github.com/Ashes47/braids/releases">releases page</a>: unzip
  it and put <code>braids.exe</code> somewhere on your PATH. There is no
  one-line installer, because the one braids has is a POSIX shell script and it
  will not ship a PowerShell one it has never run.
</p>
<p>
  Everything reads and searches the same. Two things are not offered rather
  than offered broken: <kbd>o</kbd> does not open a terminal through
  <code>BRAIDS_SPAWN</code>, because that template runs through a shell and
  quoting it for <code>cmd.exe</code> is a different set of rules that braids
  cannot test, and <kbd>v</kbd> does not update, because there is no installer
  to run. Both fall back to copying, which is what braids already does for any
  terminal it cannot drive. The tests run on Windows in CI.
</p>

<h3>Staying current</h3>
<p>
  Run the same line again to update. It asks the binary it would replace what
  version it is and stops if there is nothing to do, and it replaces braids
  where it already lives, so a build installed with Go is not shadowed by a
  second copy in <code>/usr/local/bin</code>.
</p>
<p>
  braids cannot tell you that a newer version exists, because finding out means
  asking and braids makes no network calls. It can say how long it has been
  since anyone checked, which it reads from two local facts: when this binary
  arrived, and when the installer last ran. After a month the map's version row
  grows <code>· 3 months old · v to update</code>, and <kbd>v</kbd> runs the
  installer, printing the command before it does. Checking and finding yourself
  current resets the clock, so it asks at most once a month and never claims
  you are behind.
</p>

<h2 id="first">The first index</h2>
{sh('''
braids index
''')}
<p>
  This reads every transcript under <code>~/.claude/projects</code> and writes
  one SQLite file to <code>~/.braids/index.db</code>. On a corpus of 28
  conversations and about 360 MB of transcripts it takes about eight seconds.
  Every run after that reads only what changed, which is single-digit
  milliseconds, and the map re-indexes on its own while it is open.
</p>
<p>
  Nothing else creates the index. A read command pointed at a database that is
  not there says so rather than making an empty one and answering
  <em>no matches</em>, which is a wrong answer wearing the shape of a right
  one.
</p>
{sh('''
braids search anything --db /nope/absent.db
# braids: no index at /nope/absent.db (run: braids index)
''')}

<h2 id="tour">Five minutes in</h2>
{frame("map", cmd="braids", caption="The map. Branches sit under the conversation they were cut from, tagged with the turn they left at.")}
<p>Then, in order of how often you will want them:</p>
{keys([
    ("↵", "open the conversation under the cursor, turn by turn"),
    ("/", "search everything, from any screen"),
    ("n", "jump to the next conversation waiting on you"),
    ("w", "what this session wrote to disk, heaviest first"),
    ("M", "what this project remembers"),
    ("y", "copy the <code>claude --resume</code> command for this conversation"),
    ("q", "quit"),
])}
<p>
  Nothing here is a command you have to memorise. The header of every screen
  lists every key that screen takes, and the glyph key next to it names every
  mark on the screen.
</p>

<h2 id="pages">Everything else</h2>
<p class="sub">Nine more pages, in the order they make sense in.</p>
{{INDEX_CARDS}}

<h2 id="where">Where things live</h2>
{table(["Path", "What it is", "Who owns it"], [
    ("~/.claude/projects", "your transcripts, one file per session", "Claude Code"),
    ("~/.claude/projects/&lt;p&gt;/memory", "what a project remembers", "Claude Code"),
    ("~/.claude/jobs", "the scratch files a session wrote", "Claude Code"),
    ("~/.braids/index.db", "the search index, mode 0600", "braids"),
    ("~/.braids/*.json", "names, archive flags, fork origins", "braids"),
    ("~/.braids/bin", "what braids has deleted, for 14 days", "braids"),
])}
"""

MAP = f"""
<h1>The map and the spine</h1>
<p class="lede">
  Two screens do most of the work. The map is every conversation you have. The
  spine is one of them, reduced to what happened in it.
</p>

<h2 id="map">The map</h2>
{frame("map", cmd="braids")}
<p>
  One row per conversation. A branch is indented under its parent and tagged
  with the turn it left at, so <code>← t14</code> means this conversation
  starts from turn 14 of the row above it. The columns are the title, the fork
  point, how many turns, how big the transcript is, how much scratch the
  session wrote, how long ago it was touched, and what state it is in.
</p>
<p>
  State is the column to scan. <strong>your turn</strong> means the session
  asked you something and stopped. <strong>unanswered</strong> means the last
  thing in the transcript is yours. <kbd>n</kbd> and <kbd>N</kbd> walk between
  them, so a morning can start by pressing <kbd>n</kbd> until it runs out.
</p>
{keys([
    ("j / k", "down, up"),
    ("↵", "open the spine"),
    ("/", "search everything"),
    ("f", "filter this list by title"),
    ("n / N", "next, previous conversation waiting on you"),
    ("r", "rename this conversation"),
    ("a / A", "archive this one, show archived ones"),
    ("d", "move this conversation to the bin"),
    ("w / D", "browse its work products, or bin all of them"),
    ("u", "open the bin"),
    ("M", "open memories"),
    ("y / o", "copy the resume command, or open it in a terminal"),
    ("q", "quit"),
    ("v", "run the installer, when the header offers it"),
])}

<h2 id="spine">The spine</h2>
{frame("spine", cmd="braids", then="<kbd>↵</kbd> on a conversation")}
<p>
  A long conversation is mostly turns you do not need to see again. The spine
  keeps the landmarks and collapses the rest into a run marker that counts what
  it swallowed. A conversation of 25,000 turns becomes a few hundred rows, and
  reloading it takes about 30 ms.
</p>
{table(["Mark", "What it means"], [
    ("●", "a turn: yours in the bright colour, the model's in the dim one"),
    ("⋯", "a run of turns nothing notable happened in, with how many"),
    ("◆", "a subagent this conversation spawned"),
    ("⚠", "a tool call that failed"),
    ("≈≈", "the context was compacted here"),
    ("← t14", "a branch leaves here, at turn 14"),
])}
<p>
  <kbd>n</kbd> stops at each of those marks in turn, which is the fastest way
  through a conversation you have not read in a week.
</p>
{keys([
    ("j / k", "down, up"),
    ("↵", "follow the branch or subagent on this row"),
    ("b", "branch a new conversation here"),
    ("m", "merge a branch back into this one"),
    ("p", "promote the subagent on this row"),
    ("f", "filter turns by text"),
    ("n / N", "next, previous mark"),
    ("y / o", "copy the resume command, or open it in a terminal"),
    ("esc", "back to the map"),
])}

<h3>The version row</h3>
<p>
  The header names the build always, and once nobody has checked for updates in
  a month it grows <code>· 3 months old · v to update</code>. The key rides in
  that row rather than in the legend on purpose: a fifteenth binding would need
  a third column of hints, and that column costs the glyph key, which names
  marks that are on the screen right now.
</p>
{say("<kbd>v</kbd> runs the installer, which is the only thing in braids that reaches the network, and it prints the command before running it. See <a href='/docs/privacy/'>Files and privacy</a>.")}

<h2 id="live">It keeps up on its own</h2>
<p>
  The map watches the transcript directory, the memory directories and the
  index. When a session writes a turn while you are looking at it, braids reads
  only the bytes that were added, off the drawing thread, and the row updates
  in place. Reading the whole file again would take about 3.3 seconds on a
  145 MB conversation. Reading the tail takes about 40 ms.
</p>
{say("If you sit on the spine of a live conversation, new turns appear as they land. The cursor stays where you put it.")}

<h2 id="filter">Filter is not search</h2>
<p>
  <kbd>f</kbd> filters the list in front of you, by substring, and every screen
  has it. <kbd>/</kbd> opens search across everything you have indexed. They
  are different tools and both are always available. When a filter is open the
  header says so, because a screen quietly eating your keystrokes is worse than
  no filter at all.
</p>

<h2 id="print">Printing a frame, and awkward terminals</h2>
{sh('''
braids --print                  # one frame of the map to stdout
braids --print --lane 46360382  # that conversation's spine
braids --print --query lock     # the search screen for a query
braids --print --width 195 --height 24
braids --ascii                  # narrow glyphs, for terminals that draw the box
                                # characters double width
''')}
<p>
  Print mode is how the screenshots in these docs are made, so what you see
  here is what the program draws. Set <code>BRAIDS_ASCII</code> to make the
  narrow glyphs the default.
</p>
"""

SEARCH = f"""
<h1>Search</h1>
<p class="lede">
  One field over every message you have ever sent or received, every memory a
  project keeps, and the name of every file a session left on disk.
</p>

<h2 id="screen">The screen</h2>
{frame("search", cmd="braids", then="<kbd>/</kbd> and a word")}
<p>
  <kbd>/</kbd> opens it from any screen. Results come back as you type, in a
  millisecond or two. The <strong>TYPE</strong> column says what each hit is,
  because the three kinds want different things done to them, and
  <kbd>↵</kbd> opens the screen that can do it: a conversation opens its spine
  at that turn, a memory opens the reader, a work product opens the browser at
  that directory.
</p>
{keys([
    ("type", "the query, live"),
    ("tab", "narrow to this conversation, or widen back to everything"),
    ("↑ / ↓", "move through the hits"),
    ("↵", "open the hit"),
    ("esc", "back where you came from"),
])}

<h2 id="cli">From the command line</h2>
{sh('''
braids search "lock across a network call"
braids search worktree --limit 5
braids search "index" --type memory,artifact
braids search deploy --project braids --since 30d
braids search "rate limit" --since 2026-08-01 --until 2026-08-31
braids search "timed out" --kind tool_result --lane 46360382
braids search "fork" --json
''')}
{table(["Flag", "What it does"], [
    ("--lane ID", "only this conversation, by ID prefix"),
    ("--project NAME", "only this project, matched without regard to case"),
    ("--since WHEN", "a date or an age: <code>2026-08-01</code>, <code>30d</code>, <code>6w</code>, <code>12h</code>"),
    ("--until WHEN", "the same, as an upper bound. A bare date means all of that day"),
    ("--type LIST", "conversation, memory, artifact. All three unless you narrow it"),
    ("--kind LIST", "text, thinking, tool_use, tool_result"),
    ("--limit N", "how many hits, default 20"),
    ("--json", "machine readable, with whole IDs"),
])}

<h2 id="ranking">How it ranks, and why that took two goes</h2>
<p>
  The index is SQLite FTS5 and the ranking is bm25, which prefers short
  documents. A filename is three words and a turn is several hundred, so ranked
  in one pool the filenames won everything and buried the conversations. Each
  kind is now queried and ranked separately, then merged by interleaving.
</p>
<p>
  The first attempt at that shared one query across memories and work-product
  names, which starved the memories: a search for a word appearing in four
  memories returned none of them. Each kind gets its own query now.
</p>
{say("Work products are matched <strong>by name</strong>, not by contents. Indexing the contents of 3.3 GB of scratch files would cost more than it is worth, and the name is what you remember.")}
"""

BRANCHING = f"""
<h1>Branching, merging, promoting</h1>
<p class="lede">
  Three ways to turn part of a conversation into a conversation of its own.
  None of them touch the transcript they read.
</p>

<h2 id="branch">Branch</h2>
<p>
  Put the cursor on a turn in the spine and press <kbd>b</kbd>, or:
</p>
{sh('''
braids branch --lane 46360382 --at 491
braids branch --lane 46360382 --at 491 --name "try option c"
braids branch --lane 46360382 --at 491 --workspace
''')}
<p>
  braids writes a new transcript holding that turn's ancestry from the root, and
  nothing after it, then gives you the <code>claude --resume</code> command for
  it. The source file is opened read only. A test hashes it before and after to
  prove it.
</p>
<p>
  Records are copied as they were, including the environment they captured: the
  working directory, the git branch, the file snapshots. A branch resumes
  believing in the world as it was when that turn happened, which is deliberate.
  The harness already refuses an edit built on a stale read, and that is the
  only point where the difference can do harm.
</p>

<h3 id="workspace">A workspace of its own</h3>
<p>
  <code>--workspace</code> gives the branch a git worktree, so two branches can
  edit the same file without seeing each other. braids refuses rather than
  half-doing it when a worktree cannot exist: the conversation's directory is
  not a git repository, or the branch name is taken.
</p>

<h2 id="merge">Merge</h2>
{sh('''
braids merge --lane 46360382 --from a1b2c3d4 --plan   # what would come over
braids merge --lane 46360382 --from a1b2c3d4
''')}
<p>
  This writes a third conversation: the base in full, then the turns that
  happened on the branch. It is a splice of real messages, not a summary of
  them. Neither original is touched.
</p>
<p>
  The new records get fresh IDs, because two conversations sharing a message ID
  is exactly what braids reads as a fork, and reusing them would leave a lane
  that looks like a branch of the thing it was merged from. braids refuses when
  one side already contains the other, rather than producing a duplicate under a
  new name. <code>--plan</code> reports the turn counts and stops.
</p>

<h2 id="promote">Promote a subagent</h2>
{sh('''
braids agents --lane 46360382
braids promote --lane 46360382 --agent 9f2a1c
''')}
<p>
  A subagent already holds a complete, linear exchange. It is only marked as a
  sidechain and filed under its parent. Clearing that mark and giving it a
  session ID of its own is enough for the harness to resume it, so an exchange
  that was visible as a single tool call becomes a conversation you can read,
  search and branch like any other. Its own transcript is left alone.
</p>

<h2 id="resume">Carrying on in a terminal</h2>
<p>
  <kbd>y</kbd> copies the resume command for the row under the cursor.
  <kbd>o</kbd> opens it. braids drives tmux and iTerm2 directly. Anywhere else,
  set a template:
</p>
{sh('''
export BRAIDS_SPAWN='tmux new-window -c {dir} -n {name} {cmd}'
''')}
<p>
  <code>{{cmd}}</code>, <code>{{name}}</code> and <code>{{dir}}</code> are
  filled in. Every value is shell quoted when it is substituted, so do not put
  quotes around a placeholder yourself. That quoting is not cosmetic: a
  conversation titled with a backtick used to reach <code>sh -c</code>
  unquoted.
</p>
{say("With no template and no supported terminal, <kbd>o</kbd> copies the command instead of guessing at your setup.")}
"""

MEMORIES = f"""
<h1>Memories</h1>
<p class="lede">
  What a project has told the harness to remember, plus the two problems you
  cannot see from inside a session.
</p>

<h2 id="what">What they are</h2>
<p>
  Claude Code keeps per-project memories as markdown files under
  <code>~/.claude/projects/&lt;project&gt;/memory/</code>, with a
  <code>MEMORY.md</code> index listing them. Each file has frontmatter naming
  it, describing it and giving it a type, and bodies link to each other with
  <code>[[name]]</code>.
</p>
{frame("memories", cmd="braids", then="<kbd>M</kbd>")}
<p>Press <kbd>M</kbd> on the map, or:</p>
{sh('''
braids memories
braids memories --project braids
braids memories --json
''')}

<h2 id="marks">The two things braids can see that you cannot</h2>
{table(["Mark", "What it means", "Why it matters"], [
    ("⊘", "in the directory, missing from MEMORY.md",
     "the index is what gets loaded, so nothing ever reads this file"),
    ("⊕", "a <code>[[link]]</code> to a memory that was never written",
     "the note meant to point somewhere and does not"),
])}
<p>
  Both are invisible from inside a session, which is the point of showing them
  here. A memory that is not in the index is a note you wrote and the model will
  never see.
</p>

<h2 id="read">Reading one</h2>
{frame("memory", cmd="braids", then="<kbd>M</kbd> then <kbd>↵</kbd>")}
<p>
  <kbd>↵</kbd> opens the memory with its markdown rendered: headings, emphasis,
  lists, code, links. <kbd>c</kbd> jumps to the conversation that wrote it, when
  the frontmatter recorded one.
</p>
{say("Emphasis follows the same two rules a markdown renderer needs to be usable on real notes: <code>2 * 3</code> is arithmetic, not italics, and <code>snake_case_names</code> keep their underscores.")}

<h2 id="curate">Changing them</h2>
{keys([
    ("r", "rename, following the name everywhere it appears"),
    ("i", "repair the index for this memory"),
    ("d", "delete it to the bin"),
    ("n / N", "next, previous marked memory"),
    ("c", "the conversation that wrote it"),
    ("f", "filter the list"),
])}
<p>
  A rename is four edits in one: the filename, the <code>name:</code> in the
  frontmatter, the row in <code>MEMORY.md</code>, and every
  <code>[[link]]</code> in every other memory that pointed at it. A repair adds
  a missing row to the index, or reports the loose link it cannot fix by
  itself, since braids will not invent a memory you have not written.
</p>
{say("braids preserves the file mode it found. An earlier version tightened memory files from 0644 to 0600 as a side effect of rewriting them, which is not braids' decision to make.")}
"""

WORK = f"""
<h1>Work products and the bin</h1>
<p class="lede">
  The scratch files a session writes usually dwarf its transcript. 3.3 GB
  against 360 MB on the machine these numbers come from.
</p>

<h2 id="browse">Browsing them</h2>
<p>
  Press <kbd>w</kbd> on a conversation. braids opens what that session wrote
  under <code>~/.claude/jobs</code>, heaviest first, with directories weighed
  by everything under them.
</p>
{frame("work", cmd="braids", then="<kbd>w</kbd> on a conversation")}
{keys([
    ("↵", "descend into a directory, or peek at a file"),
    ("d", "move this entry to the bin"),
    ("f", "filter this level"),
    ("esc", "up a level, then back to the map"),
    ("y", "copy the path, while reading a file"),
])}
{frame("file", cmd="braids", then="<kbd>w</kbd> then <kbd>↵</kbd> on a file")}
<p>
  Peeking shows the head of a text file, up to 128 KB. A binary is named and its
  size reported rather than spewed at your terminal. The harness's own record of
  a job is shown and refused, because deleting it would confuse the thing that
  wrote it.
</p>

<h2 id="cli">From the command line</h2>
{sh('''
braids work --lane 46360382
braids work --lane 46360382 --path tmp
braids work --lane 46360382 --json
braids work --orphans            # sets whose conversation is gone
braids work --orphans --reclaim  # move those to the bin
''')}

<h2 id="bin">The bin</h2>
<p>
  Nothing braids deletes is gone. It moves aside with a manifest and sits in
  <code>~/.braids/bin</code> for 14 days. Press <kbd>u</kbd> from the map.
</p>
{frame("bin", cmd="braids", then="<kbd>u</kbd>")}
{keys([
    ("↵ / r", "restore it to where it came from"),
    ("d", "delete it for good"),
    ("f", "filter"),
    ("esc", "back"),
])}
<p>
  Binned files keep the directory structure they came from. They used to be
  named by basename alone, which silently lost one of two files called
  <code>data.json</code>. That is a data-loss bug, so there is a test that
  bins two files of the same name and reads both back.
</p>
"""

HOOKS = f"""
<h1>Hooks</h1>
<p class="lede">
  Optional. braids works without them, and says which state it is in on every
  screen.
</p>

<h2 id="why">What a hook adds</h2>
<p>
  Files cannot tell you the difference between a session that is running and one
  that has stopped to ask your permission. Both look like a transcript that has
  not changed in a while. A hook can, because the harness tells it.
</p>
<p>
  With the hook installed, the map's state column separates
  <strong>stopped, needs you</strong> from <strong>working</strong> from
  <strong>idle</strong>, and <kbd>n</kbd> walks between the ones waiting on you.
  Without it, braids falls back to what the transcript can prove: whose turn was
  last, and how long ago. The <code>Hooks:</code> line in the header says which
  you are getting.
</p>

<h2 id="install">Installing and removing</h2>
{sh('''
braids hooks             # what is installed now
braids hooks --install
braids hooks --remove
braids hooks --json
''')}
<p>
  This edits <code>~/.claude/settings.json</code> and subscribes to six events:
  <code>PermissionRequest</code>, <code>Notification</code>, <code>Stop</code>,
  <code>SubagentStop</code>, <code>SessionStart</code> and
  <code>SessionEnd</code>. Use <code>--settings</code> to point at another file.
</p>
{say("It merges. Hooks you already have on those events are left exactly where they are, and <code>--remove</code> takes back only the entries braids added, entry by entry.")}

<h2 id="identity">Identified by program, not by path</h2>
<p>
  A braids hook is recognised by the program name in its command, not by the
  full path. Otherwise a binary at <code>~/go/bin/braids</code> and one built in
  a checkout are two different hooks: the repo build reports
  <em>not installed</em> while the installed one is reporting, and
  <code>--install</code> adds a second entry that does the same job.
</p>

<h2 id="hook">braids hook is not for you</h2>
<p>
  <code>braids hook</code> is the command the harness runs. It reads a JSON
  event on stdin and records it. Typed by hand it used to sit there waiting for
  input that was never coming, so it now refuses a terminal and says what it is
  for.
</p>
{sh('''
braids hook
# braids: hook reads an event on stdin and is run by Claude Code, not by hand
# (try: braids hooks)
''')}
"""

AGENTS = f"""
<h1>Driving braids from an agent</h1>
<p class="lede">
  Every command that reports something takes <code>--json</code> with whole IDs,
  so Claude Code can search its own past conversations and branch from the turn
  it finds.
</p>

<h2 id="json">The shape of it</h2>
<p>
  <code>--json</code> prints one object to stdout and nothing else. IDs are
  whole, never the shortened form the screens show, so a result can be fed
  straight into the next command. Errors go to stderr and the exit status is 1.
</p>
{sh('''
braids search "fork direction birth time" --limit 1 --json
''')}
<pre class="sh">{{
  "query": "fork direction birth time",
  "hits": [
    {{
      "of": "conversation",
      "lane": "46360382-7b08-436a-a8db-e0a30768c83a",
      "lane_title": "Agent observability and branching conversations",
      "project": "braids",
      "message": "be0e8453-7b94-4c86-87ba-7909c3354ae0",
      "turn": 491,
      "kind": "text",
      "role": "user",
      "snippet": "...",
      "at": "2026-09-04T10:13:01+02:00"
    }}
  ]
}}</pre>
<p>
  A hit of <code>"of": "memory"</code> carries <code>name</code> and
  <code>path</code> instead of a turn. One of <code>"of": "artifact"</code>
  carries the path of the file whose name matched.
</p>

<h2 id="loop">The loop that makes it useful</h2>
{sh('''
# 1. find the moment
braids search "the lock is held across a network call" --limit 1 --json

# 2. branch from the turn that came back
braids branch --lane 46360382 --at 491 --name "narrow the lock" --json

# 3. carry on there
claude --resume <the id branch printed>
''')}
<p>
  That is the whole trick: search returns a lane and a turn, branch takes a lane
  and a turn, and the branch is a conversation the harness can resume. An agent
  can go back to the exact moment a decision was made and take the other road,
  without disturbing the conversation it is having now.
</p>

<h2 id="explain">Where did this file come from</h2>
{sh("""
braids explain internal/core/index/index.go --json
""")}
<p>
  This joins two things braids and git each half know: git knows when a file
  changed, braids knows what was being said in that directory at the time. For
  each commit it names the conversations that were live in the window before
  it, how many turns they had, and the last thing actually said, with the whole
  lane id so the next command can use it.
</p>
{say("It does not claim the conversation caused the commit, and neither should anything reading it. What it offers is where to look, which is the honest thing braids can compute without reading a word of meaning.")}

<h2 id="checks">Check before you rely on it</h2>
{sh('''
braids hooks --json    # .reporting says whether waiting states are trustworthy
braids lanes --json    # every conversation, with whole IDs and resume commands
braids agents --lane 46360382 --json
braids work --lane 46360382 --json
braids memories --json
''')}
{say("<code>braids index</code> before a search in a long-running agent. Reads never create or migrate the index, so an agent that has never indexed gets told to, rather than getting an empty answer that looks like a real one.")}

<h2 id="exit">Exit status</h2>
{table(["Status", "When"], [
    ("0", "it worked, or you asked for help"),
    ("1", "anything else, with the reason on stderr"),
])}
<p>
  A mistyped command names the one you probably meant and exits 1.
</p>
"""

PRIVACY = f"""
<h1>Files and privacy</h1>
<p class="lede">
  braids reads every message you have exchanged with Claude Code. That is worth
  being precise about.
</p>

<h2 id="network">It makes no network calls</h2>
<p>
  There is no HTTP client and no listener in it. You do not have to take that on
  trust:
</p>
{sh('''
go list -deps ./cmd/braids | grep net/http
# (no output)
''')}
{say("braids does not call a model and has no API key. The only program that talks to Anthropic on your machine is Claude Code itself.")}
<p>
  One thing does reach the network, and only when you ask it to: the installer.
  Pressing <kbd>v</kbd> on the map runs the same
  <code>curl … | sh</code> line the docs print, in your terminal, after
  printing it. braids does not fetch anything itself, has no timer, and never
  checks anything in the background.
</p>

<h2 id="files">What it writes</h2>
{table(["Path", "What", "Mode"], [
    ("~/.braids", "everything braids owns", "0700"),
    ("~/.braids/index.db", "the search index, holding message text", "0600"),
    ("~/.braids/*.json", "names you chose, archive flags, fork origins", "0600"),
    ("~/.braids/bin", "deleted files, kept 14 days", "0700"),
])}
<p>
  The index holds the full text of every message it has read, so it is created
  private and tightened on every open. An earlier version left it
  world readable at 0644.
</p>

<h2 id="never">What it never writes</h2>
<p>
  braids never writes to a transcript the harness owns. Branching, merging and
  promoting all write new files. The sources are opened read only, and the tests
  hash them before and after.
</p>
<p>
  It also leaves file modes alone. Curating memories rewrites markdown files,
  and it preserves the mode each file had rather than imposing one.
</p>

<h2 id="delete">Deleting braids</h2>
{sh('''
braids hooks --remove
rm "$(command -v braids)"
rm -rf ~/.braids
''')}
<p>
  Everything braids knows is derived from <code>~/.claude</code>. Remove it and
  you lose a view, never a conversation.
</p>
"""

REFERENCE = f"""
<h1>Reference</h1>
<p class="lede">Every command, every flag, every environment variable.</p>

<h2 id="commands">Commands</h2>
{table(["Command", "What it does"], [
    ("braids", "open the map"),
    ("braids index", "index new and changed transcripts"),
    ("braids search QUERY", "search conversations, memories and work-product names"),
    ("braids lanes", "list indexed conversations"),
    ("braids branch", "cut a new conversation at a turn"),
    ("braids merge", "bring a branch back, as a new conversation"),
    ("braids promote", "turn a subagent into its own conversation"),
    ("braids agents", "list the subagents a conversation spawned"),
    ("braids work", "browse what a session wrote to disk"),
    ("braids memories", "what a project remembers, and what it has lost"),
    ("braids explain FILE", "which conversations were live when this file last changed"),
    ("braids hooks", "install, remove or inspect the hook"),
    ("braids version", "the version, the commit, and how old this build is"),
    ("braids help", "everything, briefly"),
])}

<h2 id="flags">Flags, by command</h2>
<h3>Common</h3>
{table(["Flag", "What it does"], [
    ("--db PATH", "index location. Default <code>$BRAIDS_DB</code> or <code>~/.braids/index.db</code>"),
    ("--json", "machine readable output, with whole IDs"),
    ("--help", "the flags that command takes"),
])}
<h3>braids (the map)</h3>
{table(["Flag", "What it does"], [
    ("--ascii", "narrow glyphs, for terminals that draw box characters double width"),
    ("--print", "render one frame to stdout instead of opening the map"),
    ("--lane ID", "with --print, that conversation's spine"),
    ("--query Q", "with --print, the search screen for a query"),
    ("--width N", "frame width when printing, default 92"),
    ("--height N", "frame height when printing, default 24"),
])}
<h3>braids index</h3>
{table(["Flag", "What it does"], [
    ("--root DIR", "transcript root. Default <code>~/.claude/projects</code>"),
    ("--full", "re-read every transcript instead of only what changed"),
])}
<h3>braids search</h3>
{table(["Flag", "What it does"], [
    ("--lane ID", "restrict to one conversation"),
    ("--project NAME", "restrict to one project"),
    ("--since WHEN", "a date or an age: 2026-08-01, 30d, 6w, 12h"),
    ("--until WHEN", "the same, as an upper bound"),
    ("--type LIST", "conversation, memory, artifact"),
    ("--kind LIST", "text, thinking, tool_use, tool_result"),
    ("--limit N", "maximum hits, default 20"),
])}
<h3>braids branch</h3>
{table(["Flag", "What it does"], [
    ("--lane ID", "conversation to branch from"),
    ("--at TURN", "turn number to branch at"),
    ("--name NAME", "name for the new conversation"),
    ("--workspace", "give the branch a git worktree of its own"),
])}
<h3>braids merge</h3>
{table(["Flag", "What it does"], [
    ("--lane ID", "conversation to carry on from"),
    ("--from ID", "branch whose turns are brought over"),
    ("--name NAME", "name for the merged conversation"),
    ("--plan", "report what would come over, and stop"),
])}
<h3>braids explain</h3>
{table(["Flag", "What it does"], [
    ("--limit N", "how many commits to look back over, default 5"),
    ("--window D", "how long before a commit a turn still counts, default 3h"),
])}

<h3>braids promote, agents, work, memories, hooks</h3>
{table(["Flag", "What it does"], [
    ("--lane ID", "promote, agents, work: the conversation"),
    ("--agent ID", "promote: the subagent to promote"),
    ("--path SUB", "work: directory within them to list"),
    ("--orphans", "work: sets whose conversation is gone"),
    ("--reclaim", "work: with --orphans, move them to the bin"),
    ("--project NAME", "memories: only this project"),
    ("--root DIR", "memories: transcript root"),
    ("--install", "hooks: ask sessions to report when they block"),
    ("--remove", "hooks: stop asking"),
    ("--settings PATH", "hooks: settings file, default <code>~/.claude/settings.json</code>"),
])}

<h2 id="env">Environment</h2>
{table(["Variable", "What it does"], [
    ("BRAIDS_DB", "where the index lives"),
    ("BRAIDS_ASCII", "set to anything to make <code>--ascii</code> the default"),
    ("BRAIDS_SPAWN", "command template for <kbd>o</kbd>, understanding <code>{{cmd}} {{name}} {{dir}}</code>"),
    ("BRAIDS_VERSION", "installer only: pin a version"),
    ("BRAIDS_BIN_DIR", "installer only: where the binary lands"),
])}

<h2 id="trouble">When something is wrong</h2>
{table(["What you see", "What it means"], [
    ("no index at PATH (run: braids index)",
     "nothing has indexed yet, or --db points somewhere else. Reads never create one"),
    ("no conversations indexed yet",
     "the index exists but is empty. Run <code>braids index</code>"),
    ("the box characters are double width",
     "run with <code>--ascii</code>, or set <code>BRAIDS_ASCII</code>"),
    ("Hooks: not installed",
     "waiting states fall back to what the transcript can prove. <code>braids hooks --install</code>"),
    ("hook reads an event on stdin",
     "you typed <code>braids hook</code>. You wanted <code>braids hooks</code>"),
    ("N conversations grew and produced no new messages",
     "braids could read them before and cannot now, which is what a format "
     "change looks like while it happens. <code>braids index</code> names them"),
    ("N conversations hold bytes braids could not read",
     "a transcript yielded no messages at all, which usually means the format "
     "changed. <code>braids index</code> names the files. Please report it"),
    ("a work product is refused",
     "it is the harness's own record of the job, and deleting it would confuse the writer"),
])}
"""


# --------------------------------------------------------------------- build

PAGES = [
    ("", "Start here", "braids docs: start here",
     "Install braids, index your Claude Code transcripts, and find your way "
     "around the map in five minutes.", START),
    ("map", "The map and the spine", "The map and the spine",
     "Every conversation as a graph, and one conversation reduced to what "
     "happened in it. Every key both screens take.", MAP),
    ("search", "Search", "Searching everything braids has indexed",
     "One query over every message, tool call, project memory and "
     "work-product name, and how the ranking works.", SEARCH),
    ("branching", "Branching and merging", "Branching, merging, promoting",
     "Cut a new conversation at any turn, give it a git worktree, bring a "
     "branch back, or promote a subagent.", BRANCHING),
    ("memories", "Memories", "Memories, and curating them",
     "What a project remembers, the two problems you cannot see from inside a "
     "session, and how to fix them.", MEMORIES),
    ("work", "Work products", "Work products and the bin",
     "Browse the scratch files a session wrote, heaviest first, and get back "
     "anything you bin.", WORK),
    ("hooks", "Hooks", "Hooks, and working without them",
     "What one hook adds, how it merges with the hooks you have, and what "
     "braids falls back to without it.", HOOKS),
    ("agents", "For agents", "Driving braids from an agent",
     "Every reporting command takes --json with whole IDs, so an agent can "
     "search its own past conversations and branch from what it finds.", AGENTS),
    ("privacy", "Files and privacy", "Files and privacy",
     "What braids reads, what it writes, what it never writes, and how to "
     "check the no-network claim yourself.", PRIVACY),
    ("reference", "Reference", "Command and flag reference",
     "Every command, every flag, every environment variable, and what to do "
     "when something looks wrong.", REFERENCE),
]

HEADING = re.compile(r'<h2 id="([^"]+)">(.*?)</h2>', re.S)


def index_cards() -> str:
    """Every page but the first, as cards on the docs home."""
    out = ""
    for slug, label, _, description, _ in PAGES[1:]:
        out += f'<a href="/docs/{slug}/"><b>{label}</b><span>{description}</span></a>'
    return f'<div class="cards">{out}</div>'


def sidebar(active: str, body: str) -> str:
    """Every page, with the current one's headings nested under it.

    Two lists, each with its own accent marker, read as two competing answers
    to "where am I". Nested, the page is the place and the heading is the spot
    within it, so one highlight can sit under the other without arguing.
    """
    out = ""
    for slug, label, _, _, _ in PAGES:
        on = " on" if slug == active else ""
        href = f"/docs/{slug}/" if slug else "/docs/"
        out += f'<a class="page{on}" href="{href}">{label}</a>'
        if slug != active:
            continue
        found = [(i, re.sub(r"<[^>]+>", "", t).strip()) for i, t in HEADING.findall(body)]
        if found:
            out += '<div class="onthis">' + "".join(
                f'<a href="#{i}">{t}</a>' for i, t in found) + "</div>"
    return f'<aside class="side"><div class="group"><h6>Docs</h6>{out}</div></aside>'


def pager(index: int) -> str:
    """Previous and next, so the docs read as one thing in order."""
    out = ""
    if index > 0:
        slug, label = PAGES[index - 1][0], PAGES[index - 1][1]
        href = f"/docs/{slug}/" if slug else "/docs/"
        out += f'<a class="prev" href="{href}"><span>Previous</span>{label}</a>'
    if index < len(PAGES) - 1:
        slug, label = PAGES[index + 1][0], PAGES[index + 1][1]
        out += f'<a class="next" href="/docs/{slug}/"><span>Next</span>{label}</a>'
    return f'<div class="pager">{out}</div>' if out else ""


# SPY keeps the sidebar in step with the reader. Written out rather than
# pulled in, because a docs page that fetches a library to underline a heading
# is a page that phones home to underline a heading.
SPY = """
<script>
(function () {
  var heads = [].slice.call(document.querySelectorAll('article h2[id]'));
  var links = {};
  [].slice.call(document.querySelectorAll('.onthis a')).forEach(function (a) {
    links[a.getAttribute('href').slice(1)] = a;
  });
  if (!heads.length) { return; }
  var queued = false;
  function update() {
    queued = false;
    // The heading nearest above the top of the window, which is the one you
    // are reading. The last one wins outright at the bottom of the page,
    // where nothing new can come into view.
    var current = heads[0].id;
    heads.forEach(function (h) {
      if (h.getBoundingClientRect().top <= 120) { current = h.id; }
    });
    if (window.innerHeight + window.scrollY >= document.body.scrollHeight - 4) {
      current = heads[heads.length - 1].id;
    }
    Object.keys(links).forEach(function (id) {
      links[id].classList.toggle('here', id === current);
    });
  }
  function queue() {
    if (!queued) { queued = true; requestAnimationFrame(update); }
  }
  window.addEventListener('scroll', queue, { passive: true });
  window.addEventListener('resize', queue);
  update();
})();
</script>
"""


def build() -> None:
    for i, (slug, label, title, description, body) in enumerate(PAGES):
        body = body.replace("{INDEX_CARDS}", index_cards())
        html_body = (
            nav(HOME, active="/docs/")
            + '<div class="wrap wide"><div class="docs">'
            + sidebar(slug, body)
            + f"<article>{body}{pager(i)}</article>"
            + "</div></div>"
            + footer(HOME)
            + SPY
        )
        out = OUT / slug / "index.html" if slug else OUT / "index.html"
        out.parent.mkdir(parents=True, exist_ok=True)
        page_html = page(title=f"{title} | braids", description=description,
                         body=html_body, css=DOCS_CSS, home=HOME)
        out.write_text(page_html)
        print(f"wrote {out.relative_to(HERE)}: {len(page_html) // 1024} KB")


if __name__ == "__main__":
    build()

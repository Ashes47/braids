#!/usr/bin/env python3
"""Build the braids site.

The screenshots are real braids output, produced by scripts/demo.py against a
fake ~/.claude, then pasted in verbatim. A hand-drawn mockup drifts from the
program the moment either changes, and a frame whose borders do not line up is
the first thing a reader notices.

    python3 scripts/demo.py --out /tmp/braids-demo --frames site/frames --width 88
    python3 site/build.py
"""

import pathlib

from chrome import CSS, footer, frame, nav, page

HERE = pathlib.Path(__file__).parent


BODY = f"""
{nav()}

<header><div class="wrap">
  <img class="mark" src="assets/braids-logo.png" alt="braids">
  <h1>Conversations don't go<br>in a straight line.</h1>
  <p class="lede">
    Claude Code works best on one linear thread doing one thing. You don't work
    that way — a task forks, doubles back, and spawns three side quests.
    <strong>braids is the map.</strong> It draws every conversation and every
    branch as one graph, searches all of them in milliseconds, and turns any
    message into the start of a new conversation.
  </p>
  <p class="lede">
    It never talks to a model. It arranges the conversations you have with one.
    <em>Local, open source, nothing hosted.</em>
  </p>
  <div class="cta">
    <div class="install">
      <span class="prompt">$</span>
      <span id="cmd">curl -fsSL https://braids.chat/install.sh | sh</span>
      <button class="copy" onclick="copyCmd(this)">copy</button>
    </div>
    <a class="btn primary" href="docs/">Read the docs</a>
    <a class="btn" href="https://github.com/Ashes47/braids">Star on GitHub</a>
  </div>
  <p class="note">
    One binary, no daemon, no configuration file. Or
    <code>go install github.com/Ashes47/braids/cmd/braids@latest</code> if you
    have Go.
  </p>
</div></header>

<section id="what"><div class="wrap">
  <h2><span class="kicker">The map</span>Every conversation, and where each one came from</h2>
  <p class="sub">
    Branches sit under the conversation they were cut from, with the turn they
    left at. The state column is the part you actually scan: who is owed
    something, and by whom.
  </p>
  {frame("map", "Real output. Every screenshot on this page is produced by scripts/demo.py against a fake ~/.claude, not drawn by hand.")}
</div></section>

<section><div class="wrap">
  <h2><span class="kicker">The spine</span>A long conversation, reduced to what happened in it</h2>
  <p class="sub">
    Not every line — the landmarks. What you asked, what came back, where a tool
    call failed, where the context was compacted, and where each branch left.
    A 25,000-turn conversation becomes a few hundred rows you can actually read.
  </p>
  {frame("spine", "⋯ is a run of turns nothing interesting happened in. ⚠ is a failed tool call. ← t14 is where a branch left.")}
  <p>
    Put the cursor on any turn and press <kbd>b</kbd>. braids writes a new
    session file holding that turn's ancestry and hands you the
    <code>claude --resume</code> command. Add a workspace and the branch gets a
    git worktree of its own, so two branches can edit the same file without
    touching each other.
  </p>
</div></section>

<section><div class="wrap">
  <h2><span class="kicker">Search</span>One query across everything you have</h2>
  <p class="sub">
    Every message and tool call, every memory a project keeps, and the name of
    every file a session left on disk. Each result says which it is, and
    <kbd>↵</kbd> opens the screen that can act on it.
  </p>
  {frame("search", "Ranked per kind and merged, because a filename is three words against a turn's several hundred — ranked together, filenames bury every conversation.")}
</div></section>

<section id="local"><div class="wrap">
  <h2><span class="kicker">Local</span>It reads your transcripts. So this matters.</h2>
  <ul class="plain">
    <li><strong>No network calls.</strong> There is no HTTP client and no
      listener in it. <code>go list -deps ./cmd/braids | grep net/http</code>
      comes back empty — check it yourself.</li>
    <li><strong>Nothing leaves your machine.</strong> The index is a SQLite file
      at <code>~/.braids/index.db</code>.</li>
    <li><strong>What it writes stays yours.</strong> <code>~/.braids</code> is
      <code>0700</code> and the index is <code>0600</code>, because it holds the
      full text of every message it has read.</li>
    <li><strong>It never writes to a transcript the harness owns.</strong>
      Branching writes a new file; the source is opened read-only, and a test
      hashes it before and after.</li>
    <li><strong>Delete braids and you lose a view, never a conversation.</strong>
      Everything is derived from <code>~/.claude</code>.</li>
  </ul>
</div></section>

<section><div class="wrap">
  <h2><span class="kicker">Also in the box</span>The things a session leaves behind</h2>
  <div class="cols">
    <div class="card">
      <h4>Work products</h4>
      <p>A session's scratch usually dwarfs its transcript — 3.3&nbsp;GB against
      360&nbsp;MB on one machine. <kbd>w</kbd> opens it as a size browser,
      heaviest first, so you can bin one 231&nbsp;MB dump and keep the rest.</p>
    </div>
    <div class="card">
      <h4>Memories</h4>
      <p>What a project told the harness to remember, plus the two things you
      cannot see from inside a session: a memory the index omits, so nothing
      loads it, and a link pointing at one that is gone.</p>
    </div>
    <div class="card">
      <h4>Merge and promote</h4>
      <p>Join a branch back as a new conversation, spliced from the real turns of
      both — refused when one already contains the other. A subagent's
      transcript can become a conversation of its own.</p>
    </div>
    <div class="card">
      <h4>A recoverable bin</h4>
      <p>Everything braids deletes moves aside with a manifest and a 14-day
      retention. Being wrong about a deletion costs a keystroke.</p>
    </div>
    <div class="card">
      <h4>Hooks, opt in</h4>
      <p>Files cannot tell a session that is running from one waiting on your
      approval. One hook can. It merges with whatever you already have, and
      takes back only what it added.</p>
    </div>
    <div class="card">
      <h4>Usable by an agent</h4>
      <p>Every command that reports something takes <code>--json</code>, so
      Claude Code can search its own past conversations and branch from the turn
      it finds.</p>
    </div>
  </div>
</div></section>

<section><div class="wrap">
  <h2><span class="kicker">Numbers</span>Measured, on one laptop, against a real corpus</h2>
  <p class="sub">28 conversations, ~62,000 messages, ~360&nbsp;MB of transcripts.
  Rounded, because a corpus you are still working in never sits still.</p>
  <table>
    <tr><th>Operation</th><th>Cost</th></tr>
    <tr><td>First index, from nothing</td><td>~8 s</td></tr>
    <tr><td>Re-index, a turn added to a 145 MB conversation</td><td>~40 ms</td></tr>
    <tr><td>Re-index, nothing changed</td><td>under 1 ms</td></tr>
    <tr><td>Search, across ~62,000 indexed units</td><td>1–6 ms</td></tr>
    <tr><td>Index on disk</td><td>~190 MB</td></tr>
  </table>
  <p class="note">
    The last row is the honest cost: the index is roughly half the size of the
    transcripts, because searching text means storing it again.
  </p>
</div></section>

<section id="commands"><div class="wrap">
  <h2><span class="kicker">Commands</span>The whole surface</h2>
  <table>
    <tr><th>Command</th><th>What it does</th></tr>
    <tr><td>braids</td><td>open the map</td></tr>
    <tr><td>braids index</td><td>index new and changed transcripts</td></tr>
    <tr><td>braids search QUERY</td><td>search conversations, memories and work-product names</td></tr>
    <tr><td>braids lanes</td><td>list indexed conversations</td></tr>
    <tr><td>braids branch --lane ID --at TURN</td><td>cut a new conversation at that turn</td></tr>
    <tr><td>braids merge --lane ID --from ID</td><td>join a branch back, as a new conversation</td></tr>
    <tr><td>braids promote --lane ID --agent ID</td><td>turn a subagent into its own conversation</td></tr>
    <tr><td>braids agents --lane ID</td><td>list the subagents a conversation spawned</td></tr>
    <tr><td>braids work --lane ID</td><td>browse what a session wrote to disk</td></tr>
    <tr><td>braids memories</td><td>what a project remembers, and what it has lost</td></tr>
    <tr><td>braids hooks --install</td><td>let sessions report when they block</td></tr>
  </table>
  <p class="note">
    Every command takes <code>--help</code> for its own flags and
    <code>--json</code> if it reports something. A mistyped one names the command
    you meant.
  </p>
</div></section>

<section id="star"><div class="wrap">
  <h2><span class="kicker">If it is useful</span>Star the repo</h2>
  <p class="sub">
    braids is one person's tool, made public because the problem is not one
    person's. If it saves you a terminal, a star is how the next person finds
    it — and an issue is how it gets better.
  </p>
  <div class="cta">
    <a class="btn primary" href="https://github.com/Ashes47/braids">⭐ Star braids on GitHub</a>
    <a class="btn" href="https://github.com/Ashes47/braids/issues/new/choose">Open an issue</a>
  </div>
</div></section>

<section><div class="wrap">
  <h2><span class="kicker">Install</span>One binary</h2>
<pre class="sh"><span class="c"># works out the build for your machine and verifies it</span>
curl -fsSL https://braids.chat/install.sh | sh

<span class="c"># or with Go</span>
go install github.com/Ashes47/braids/cmd/braids@latest

<span class="c"># or from source</span>
git clone https://github.com/Ashes47/braids &amp;&amp; cd braids
make install

<span class="c"># then</span>
braids index   <span class="c"># read every transcript under ~/.claude</span>
braids         <span class="c"># open the map</span>
</pre>
  <p class="note">
    Nothing is a command you have to remember: the header of every screen lists
    all of its keys.
  </p>
</div></section>

{footer()}

<script>
function copyCmd(button) {{
  navigator.clipboard.writeText(document.getElementById('cmd').textContent).then(function () {{
    var was = button.textContent;
    button.textContent = 'copied';
    setTimeout(function () {{ button.textContent = was; }}, 1400);
  }});
}}
</script>
"""

PAGE = page(
    title="braids — branch your Claude Code conversations",
    description="braids draws every Claude Code conversation and every branch as one graph, searches all of them in milliseconds, and turns any message into the start of a new conversation. Local, open source, nothing hosted.",
    og_title="braids — conversations don't go in a straight line",
    og_description="A terminal map of every Claude Code conversation and branch. Search all of them in milliseconds. Fork a new conversation from any message. Local, open source, nothing hosted.",
    body=BODY,
    css=CSS,
)

(HERE / "index.html").write_text(PAGE)
print("wrote site/index.html —", len(PAGE), "bytes")

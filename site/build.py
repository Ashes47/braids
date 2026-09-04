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
import shutil

from chrome import CSS, footer, frame, nav, page

HERE = pathlib.Path(__file__).parent


BODY = f"""
{nav()}

<header><div class="wrap">
  <img class="mark" src="assets/braids-logo.png" alt="braids">
  <h1>Claude Code runs one thread.<br>Your work runs twelve.</h1>
  <p class="lede">
    braids draws every conversation you have had, and every branch of it, as one
    graph. It searches all of them in a few milliseconds. Put the cursor on any
    message and start a new conversation from exactly there.
  </p>
  <p class="lede">
    braids never talks to a model. It arranges the conversations you have with
    one. <em>Local, open source, nothing hosted.</em>
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
    One binary. No daemon, no config file, nothing to sign up for. Prefer Go?
    <code>go install github.com/Ashes47/braids/cmd/braids@latest</code>
  </p>
</div></header>

<section id="what"><div class="wrap">
  <h2><span class="kicker">The map</span>Every conversation, and where it came from</h2>
  <p class="sub">
    Branches sit under the conversation they were cut from, tagged with the turn
    they left at. The column you actually scan is the last one: who is waiting
    on you, and what is still running.
  </p>
  {frame("map", cmd="braids", caption="A real frame at 195 columns, colours and all. Captured by scripts/demo.py against a fake ~/.claude, never drawn by hand.")}
  <ul class="plain">
    <li><strong>your turn</strong> means the session asked you something and
      stopped. <kbd>n</kbd> jumps to the next one.</li>
    <li><strong>The indent</strong> is lineage. <code>t14</code> on a branch is
      the turn of the parent it was cut at.</li>
    <li><strong>The state column</strong> is the one you scan. Four
      conversations here are waiting on you and one is still working.</li>
  </ul>
</div></section>

<section><div class="wrap">
  <h2><span class="kicker">The spine</span>A 25,000 turn conversation in a few hundred rows</h2>
  <p class="sub">
    braids keeps the landmarks and collapses the rest: what you asked, what came
    back, where a tool call failed, where the context got compacted, and where
    every branch left. Long conversations become readable.
  </p>
  {frame("spine", cmd="braids", then="<kbd>↵</kbd> on a conversation")}
  <ul class="plain">
    <li><code>⋯</code> is a run of turns where nothing notable happened. It
      counts them so you know what you are skipping.</li>
    <li><code>⚠</code> is a tool call that failed. <kbd>n</kbd> walks straight
      down the failures.</li>
    <li><code>← t14</code> is a branch leaving. Press <kbd>↵</kbd> to follow it.</li>
  </ul>
  <p>
    Put the cursor on a turn and press <kbd>b</kbd>. braids writes a new session
    file holding that turn's ancestry, then hands you the
    <code>claude --resume</code> command. Ask for a workspace and the branch
    gets its own git worktree, so two branches can edit the same file and never
    collide.
  </p>
</div></section>

<section><div class="wrap">
  <h2><span class="kicker">Search</span>One field over everything you have</h2>
  <p class="sub">
    Every message and tool call, every memory a project keeps, and the name of
    every file a session left on disk. Each hit says which kind it is. Press
    <kbd>↵</kbd> and braids opens the screen that can do something about it.
  </p>
  {frame("search", cmd="braids", then="<kbd>/</kbd> and a word")}
  <p class="note">
    Each kind is ranked on its own and then merged. Ranked together, a
    three-word filename beats a turn of several hundred words every time, and
    filenames bury the conversations you were looking for.
  </p>
</div></section>

<section id="local"><div class="wrap">
  <h2><span class="kicker">Local</span>It reads your transcripts, so this matters</h2>
  <ul class="plain">
    <li><strong>No network calls.</strong> There is no HTTP client and no
      listener in it. Run
      <code>go list -deps ./cmd/braids | grep net/http</code> and see for
      yourself.</li>
    <li><strong>Nothing leaves the machine.</strong> The index is one SQLite
      file at <code>~/.braids/index.db</code>.</li>
    <li><strong>What it writes stays yours.</strong> <code>~/.braids</code> is
      <code>0700</code> and the index is <code>0600</code>, because it holds the
      full text of every message it has read.</li>
    <li><strong>It never writes to a transcript the harness owns.</strong>
      Branching writes a new file. The source is opened read only, and a test
      hashes it before and after to prove it.</li>
    <li><strong>Delete braids and you lose a view, not a conversation.</strong>
      All of it is derived from <code>~/.claude</code>.</li>
  </ul>
</div></section>

<section><div class="wrap">
  <h2><span class="kicker">Also in the box</span>The things a session leaves behind</h2>
  <div class="cols">
    <div class="card">
      <h4>Work products</h4>
      <p>A session's scratch files usually dwarf its transcript. On one machine
      it was 3.3&nbsp;GB against 360&nbsp;MB. <kbd>w</kbd> opens it heaviest
      first, so you can bin the 231&nbsp;MB dump and keep everything else.</p>
    </div>
    <div class="card">
      <h4>Memories</h4>
      <p>What a project told the harness to remember, with the two problems you
      cannot see from inside a session: a memory missing from the index, so
      nothing ever loads it, and a link pointing at one that was never
      written.</p>
    </div>
    <div class="card">
      <h4>Merge and promote</h4>
      <p>Bring a branch back as a new conversation, spliced from the real turns
      of both. braids refuses when one already contains the other. A subagent's
      transcript can graduate into a conversation of its own.</p>
    </div>
    <div class="card">
      <h4>A bin you can undo</h4>
      <p>Everything braids deletes moves aside with a manifest and sits there
      for 14 days. Press <kbd>u</kbd> to get it back.</p>
    </div>
    <div class="card">
      <h4>Hooks, if you want them</h4>
      <p>Files cannot tell a running session apart from one waiting on your
      approval. One hook can. It merges with whatever hooks you already have,
      and removing it takes back only what it added.</p>
    </div>
    <div class="card">
      <h4>Agents can drive it</h4>
      <p>Every command that reports something takes <code>--json</code> with
      whole IDs, so Claude Code can search its own past conversations and branch
      from the turn it finds.</p>
    </div>
  </div>
</div></section>

<section><div class="wrap">
  <h2><span class="kicker">Numbers</span>Measured on one laptop, against a real corpus</h2>
  <p class="sub">28 conversations, about 62,000 messages, about 360&nbsp;MB of
  transcripts. Rounded, because a corpus you are still working in will not sit
  still.</p>
  <table>
    <tr><th>Operation</th><th>Cost</th></tr>
    <tr><td>First index, from nothing</td><td>about 8 s</td></tr>
    <tr><td>Re-index after a turn lands in a 145 MB conversation</td><td>about 40 ms</td></tr>
    <tr><td>Re-index when nothing changed</td><td>under 1 ms</td></tr>
    <tr><td>Search across about 62,000 indexed units</td><td>1 to 6 ms</td></tr>
    <tr><td>Index on disk</td><td>about 190 MB</td></tr>
  </table>
  <p class="note">
    That last row is the honest cost. The index runs about half the size of the
    transcripts, because searching text means keeping a copy of it.
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
    <tr><td>braids merge --lane ID --from ID</td><td>bring a branch back, as a new conversation</td></tr>
    <tr><td>braids promote --lane ID --agent ID</td><td>turn a subagent into its own conversation</td></tr>
    <tr><td>braids agents --lane ID</td><td>list the subagents a conversation spawned</td></tr>
    <tr><td>braids work --lane ID</td><td>browse what a session wrote to disk</td></tr>
    <tr><td>braids memories</td><td>what a project remembers, and what it has lost</td></tr>
    <tr><td>braids hooks --install</td><td>let sessions report when they block</td></tr>
  </table>
  <p class="note">
    Every command takes <code>--help</code> for its own flags, and
    <code>--json</code> if it reports anything. Mistype one and braids names the
    command you probably meant.
  </p>
</div></section>

<section id="star"><div class="wrap">
  <h2><span class="kicker">If it is useful</span>Star the repo</h2>
  <p class="sub">
    braids is one person's tool, made public because the problem is not one
    person's. A star is how the next person finds it. An issue is how it gets
    better.
  </p>
  <div class="cta">
    <a class="btn primary" href="https://github.com/Ashes47/braids">★ Star braids on GitHub</a>
    <a class="btn" href="https://github.com/Ashes47/braids/issues/new/choose">Open an issue</a>
  </div>
</div></section>

<section id="install"><div class="wrap">
  <h2><span class="kicker">Install</span>One binary, and one way out</h2>
<pre class="sh"><span class="c"># picks the build for your machine and checks it against the published sums</span>
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
  <h3>Uninstalling</h3>
<pre class="sh"><span class="c"># take the hook back out of ~/.claude/settings.json, if you added it</span>
braids hooks --remove

<span class="c"># then the binary and the index</span>
rm "$(command -v braids)"
rm -rf ~/.braids
</pre>
  <p class="note">
    That is all of it. Your conversations live in <code>~/.claude</code> and are
    never touched, so removing braids costs you the map and nothing else.
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
    title="braids: branch your Claude Code conversations",
    description="braids draws every Claude Code conversation and every branch as one graph, searches all of them in milliseconds, and turns any message into the start of a new conversation. Local, open source, nothing hosted.",
    og_title="braids: conversations don't go in a straight line",
    og_description="A terminal map of every Claude Code conversation and branch. Search all of them in milliseconds. Fork a new conversation from any message. Local, open source, nothing hosted.",
    body=BODY,
    css=CSS,
)

(HERE / "index.html").write_text(PAGE)
print("wrote site/index.html:", len(PAGE), "bytes")

# The page needs the mark and the icons. They live in assets/ at the top of the
# repo, and were once a second byte-identical copy in here, which is one logo
# to update and two places to forget. Copied at build time instead, so the
# repo holds one of each.
NEEDED = ["braids-logo.png", "braids-icon-64.png", "braids-icon-256.png"]
into = HERE / "assets"
into.mkdir(exist_ok=True)
for name in NEEDED:
    shutil.copyfile(HERE.parent / "assets" / name, into / name)
print(f"copied {len(NEEDED)} assets into site/assets")

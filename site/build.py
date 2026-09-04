#!/usr/bin/env python3
"""Build the braids site.

The screenshots are real braids output, produced by scripts/demo.py against a
fake ~/.claude, then pasted in verbatim. A hand-drawn mockup drifts from the
program the moment either changes, and a frame whose borders do not line up is
the first thing a reader notices.

    make frames   # recapture every screen, then redraw the README PNGs
    python3 site/build.py
"""

import pathlib
import shutil

from chrome import CSS, capture, footer, frame, mark, nav, page

HERE = pathlib.Path(__file__).parent


BODY = f"""
{nav()}

<header><div class="wrap">
  <div class="marks">
    <img class="mark" src="assets/braids-logo.png" alt="braids">
    {mark()}
  </div>
  <h1>You worked this out already.<br>braids finds where.</h1>
  <p class="lede">
    Every Claude Code session on your machine, searchable in a few
    milliseconds: what you asked, what came back, what a tool returned, what a
    project remembers. Find the turn where you worked something out, then start
    a new conversation from exactly there.
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

<section id="find"><div class="wrap">
  <h2><span class="kicker">Find it</span>One field over everything you have</h2>
  <p class="sub">
    Every message and tool call, every memory a project keeps, and the name of
    every file a session left on disk. Each hit says which kind it is, and
    <kbd>↵</kbd> opens the screen that can do something about it: a
    conversation at that turn, a memory in the reader, a file in the browser.
  </p>
  {frame("search", cmd="braids", then="<kbd>/</kbd> and a word")}
  <p class="more"><a href="docs/search/">Scopes, kinds, dates, and how the ranking works</a></p>
</div></section>

<section id="explain"><div class="wrap">
  <h2><span class="kicker">Explain</span>Which conversation produced this file</h2>
  <p class="sub">
    git knows when a file changed. braids knows what was being said in that
    directory at the time. Joining them gets you from a line of code back to
    the conversation it came out of.
  </p>
  {capture("explain", "braids explain checkout/handler.go",
           "Real output, against a fake repository and a fake ~/.claude.")}
  <p>
    It does not claim those conversations caused those commits, and it says so
    outright. What it offers is where to look, which is the honest thing to
    compute without reading a word of meaning. That is also why it costs a git
    log and two columns rather than a model.
  </p>
  <p class="more"><a href="docs/reference/#commands">Every command braids has</a></p>
</div></section>

<section id="branch"><div class="wrap">
  <h2><span class="kicker">Then carry on</span>Start a new conversation from any turn</h2>
  <p class="sub">
    Finding the moment is half of it. The spine collapses a long conversation
    to its landmarks: what you asked, what came back, where a tool call failed,
    where the context got compacted, where each branch left.
  </p>
  {frame("spine", cmd="braids", then="<kbd>↵</kbd> on a conversation")}
  <p>
    Put the cursor on a turn and press <kbd>b</kbd>. braids writes a new session
    file holding that turn's ancestry, then hands you the
    <code>claude --resume</code> command. Ask for a workspace and the branch
    gets its own git worktree, so two branches can edit the same file and never
    collide. The transcript you branched from is opened read only, and a test
    hashes it before and after to prove it.
  </p>
  <p class="more"><a href="docs/branching/">Branching, merging and promoting a subagent</a></p>
</div></section>

<section id="map"><div class="wrap">
  <h2><span class="kicker">The map</span>And the shape of all of it</h2>
  <p class="sub">
    One row per conversation, with branches under the conversation they were cut
    from, tagged with the turn they left at. Search is the front door; this is
    where you see what you have and who is waiting on you.
  </p>
  {frame("map", cmd="braids")}
  <ul class="plain">
    <li><strong>your turn</strong> means the session asked you something and
      stopped. <kbd>n</kbd> jumps to the next one, which is a decent way to
      start a morning.</li>
    <li><strong>The indent is lineage.</strong> <code>t14</code> on a branch is
      the turn of the parent it was cut at.</li>
    <li><strong>Every screenshot here is real output</strong>, colours and all,
      captured against a fake <code>~/.claude</code>.</li>
  </ul>
  <p class="more"><a href="docs/map/">Every key on the map and the spine</a></p>
</div></section>

<section id="more"><div class="wrap">
  <h2><span class="kicker">And the rest</span>What else is in there</h2>
  <p class="sub">
    Five more screens, each with a page of its own. They are the parts of a
    session you cannot see from inside it.
  </p>
  <div class="cols">
    <div class="card">
      <h4><a href="docs/work/">Work products</a></h4>
      <p>A session's scratch usually dwarfs its transcript. On one machine it
      was 3.3&nbsp;GB against 360&nbsp;MB. <kbd>w</kbd> opens it heaviest
      first, so you can bin the 231&nbsp;MB dump and keep the rest.</p>
    </div>
    <div class="card">
      <h4><a href="docs/memories/">Memories</a></h4>
      <p>What the project remembers, with the two problems a session cannot
      show you: a memory missing from the index, so nothing ever loads it, and
      a link pointing at one that was never written.</p>
    </div>
    <div class="card">
      <h4><a href="docs/work/#bin">A bin you can undo</a></h4>
      <p>Everything braids deletes moves aside with a manifest and sits there
      for 14 days, keeping the directories it came from. Press <kbd>u</kbd> to
      get it back.</p>
    </div>
    <div class="card">
      <h4><a href="docs/branching/#merge">Merge and promote</a></h4>
      <p>Bring a branch back as a new conversation, spliced from the real turns
      of both. A subagent's transcript can graduate into a conversation of its
      own.</p>
    </div>
    <div class="card">
      <h4><a href="docs/hooks/">Hooks, if you want them</a></h4>
      <p>Files cannot tell a running session apart from one waiting on your
      approval. One hook can. It merges with the hooks you already have, and
      removing it takes back only what it added.</p>
    </div>
    <div class="card">
      <h4><a href="docs/agents/">Agents can drive it</a></h4>
      <p>Every command that reports something takes <code>--json</code> with
      whole IDs, so Claude Code can search its own past conversations and
      branch from the turn it finds.</p>
    </div>
  </div>
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
      <code>0700</code> and the index is <code>0600</code>, because it holds
      the full text of every message it has read.</li>
    <li><strong>It never writes to a transcript the harness owns.</strong>
      Branching, merging and promoting all write new files.</li>
    <li><strong>Delete braids and you lose a view, not a conversation.</strong>
      All of it is derived from <code>~/.claude</code>.</li>
  </ul>
  <p class="more"><a href="docs/privacy/">What it reads, writes, and never writes</a></p>
</div></section>

<section id="numbers"><div class="wrap">
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
    <tr><td>Index on disk</td><td>about 125 MB</td></tr>
  </table>
  <p class="note">
    That last row is the honest cost: about a third of the transcripts, because
    searching text means keeping a copy of it. Two thirds of the index is that
    copy and the search structure over it.
  </p>
</div></section>

<section id="install"><div class="wrap">
  <h2><span class="kicker">Install</span>One binary, and one way out</h2>
<pre class="sh"><span class="c"># picks the build for your machine and checks it against the published sums</span>
curl -fsSL https://braids.chat/install.sh | sh

<span class="c"># or with Go</span>
go install github.com/Ashes47/braids/cmd/braids@latest

<span class="c"># then</span>
braids index   <span class="c"># read every transcript under ~/.claude</span>
braids         <span class="c"># open the map</span>
</pre>
  <p class="note">
    Run that first line again to update. It asks the binary it would replace
    what version it is, says <em>already installed</em> and stops if there is
    nothing to do, and replaces braids where it already lives rather than
    leaving a second copy somewhere else on your PATH.
  </p>

  <h3>Staying current</h3>
  <p class="sub">
    braids cannot tell you a newer version exists, because finding out would
    mean asking, and it makes no network calls. What it can say is how long it
    has been since anyone checked. After a month the map's header adds
    <code>3 months old · v to update</code> to the version row, and
    <kbd>v</kbd> runs the installer above, printing the command first.
    Checking and finding yourself current resets the clock, so it asks once a
    month at most and never claims you are behind.
  </p>

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
  <p class="more"><a href="docs/">Install, first index, and a five minute tour</a></p>
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
    title="braids: find the reasoning in your past Claude Code sessions",
    description="Every Claude Code session on your machine, searchable in milliseconds. Find the turn where you worked something out, see which conversation produced a file, and start a new conversation from exactly there. Local, open source, nothing hosted.",
    og_title="braids: you worked this out already",
    og_description="Every Claude Code session on your machine, searchable in milliseconds, with the conversation behind any file and a new branch from any turn. Local, open source, nothing hosted.",
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
# The screenshots too, which the pages show as images and the README shares.
frames = into / "frames"
frames.mkdir(exist_ok=True)
shots = sorted((HERE.parent / "assets" / "frames").glob("*.png"))
for shot in shots:
    shutil.copyfile(shot, frames / shot.name)
print(f"copied {len(NEEDED)} assets and {len(shots)} frames into site/assets")

#!/usr/bin/env python3
"""Build the braids site.

The screenshots are real braids output, produced by scripts/demo.py against a
fake ~/.claude, then pasted in verbatim. A hand-drawn mockup drifts from the
program the moment either changes, and a frame whose borders do not line up is
the first thing a reader notices.

    python3 scripts/demo.py --out /tmp/braids-demo --frames site/frames --width 88
    python3 site/build.py
"""

import html
import pathlib

HERE = pathlib.Path(__file__).parent
FRAMES = HERE / "frames"


def frame(name: str, caption: str = "") -> str:
    text = html.escape((FRAMES / f"{name}.txt").read_text().rstrip("\n"))
    cap = f'<figcaption>{caption}</figcaption>' if caption else ""
    return f'<figure class="frame"><pre>{text}</pre>{cap}</figure>'


CSS = """
:root {
  --bg:#0b0d10; --panel:#11141a; --line:#232833; --ink:#e6edf3; --dim:#8b949e;
  --faint:#6e7681; --accent:#e07a2f; --blue:#3b93f7; --ok:#3fb950;
  --mono: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace;
  --sans: -apple-system, BlinkMacSystemFont, "Segoe UI", Inter, Helvetica, Arial, sans-serif;
}
* { box-sizing:border-box; }
html { scroll-behavior:smooth; }
body {
  margin:0; background:var(--bg); color:var(--ink);
  font-family:var(--sans); font-size:16px; line-height:1.65;
  -webkit-font-smoothing:antialiased;
}
a { color:var(--blue); text-decoration:none; }
a:hover { text-decoration:underline; }
.wrap { max-width:1000px; margin:0 auto; padding:0 24px; }

/* ---- nav ---- */
nav {
  position:sticky; top:0; z-index:10; background:rgba(11,13,16,.86);
  backdrop-filter:blur(8px); border-bottom:1px solid var(--line);
}
nav .wrap { display:flex; align-items:center; gap:20px; height:58px; }
nav img { height:26px; width:auto; display:block; }
nav .name { font-family:var(--mono); font-weight:600; letter-spacing:.02em; color:var(--ink); }
nav .spacer { flex:1; }
nav a.link { color:var(--dim); font-size:14px; }
nav a.link:hover { color:var(--ink); text-decoration:none; }

/* ---- hero ---- */
header { padding:72px 0 8px; }
header .mark { height:104px; width:auto; margin-bottom:26px; }
h1 {
  font-size:clamp(30px,4.6vw,52px); line-height:1.1; margin:0 0 18px;
  letter-spacing:-.02em; font-weight:650;
}
.lede { font-size:clamp(16px,1.6vw,19px); color:var(--dim); max-width:66ch; margin:0 0 8px; }
.lede strong { color:var(--ink); font-weight:600; }
.lede em { color:var(--ink); font-style:normal; border-bottom:1px solid var(--accent); }

.cta { display:flex; flex-wrap:wrap; gap:12px; align-items:center; margin:28px 0 6px; }
.install {
  font-family:var(--mono); font-size:13.5px; background:var(--panel);
  border:1px solid var(--line); border-radius:8px; padding:11px 14px;
  color:var(--ink); display:flex; gap:10px; align-items:center;
}
.install .prompt { color:var(--faint); }
button.copy {
  font-family:var(--sans); font-size:12px; background:transparent; color:var(--dim);
  border:1px solid var(--line); border-radius:6px; padding:4px 9px; cursor:pointer;
}
button.copy:hover { color:var(--ink); border-color:var(--faint); }
.btn {
  font-size:14px; padding:11px 16px; border-radius:8px; border:1px solid var(--line);
  color:var(--ink); background:var(--panel);
}
.btn:hover { border-color:var(--faint); text-decoration:none; }
.btn.primary { background:var(--accent); border-color:var(--accent); color:#1a1205; font-weight:600; }
.btn.primary:hover { filter:brightness(1.08); }
.note { color:var(--faint); font-size:13px; margin:10px 0 0; }

/* ---- sections ---- */
section { padding:56px 0; border-top:1px solid var(--line); }
section:first-of-type { border-top:none; }
h2 { font-size:clamp(21px,2.4vw,28px); margin:0 0 10px; letter-spacing:-.01em; }
h2 .kicker {
  display:block; font-family:var(--mono); font-size:12px; font-weight:500;
  letter-spacing:.09em; text-transform:uppercase; color:var(--accent); margin-bottom:9px;
}
h3 { font-size:17px; margin:26px 0 6px; }
p { max-width:72ch; }
p.sub { color:var(--dim); margin-top:0; }

/* ---- terminal frames ---- */
figure.frame { margin:22px 0 0; }
figure.frame pre {
  font-family:var(--mono); font-size:11.5px; line-height:1.5; margin:0;
  background:var(--panel); border:1px solid var(--line); border-radius:10px;
  padding:16px 18px; overflow-x:auto; color:var(--ink);
  -webkit-overflow-scrolling:touch;
}
figure.frame figcaption { color:var(--faint); font-size:13px; margin-top:9px; }

pre.sh {
  font-family:var(--mono); font-size:13px; line-height:1.7; margin:16px 0 0;
  background:var(--panel); border:1px solid var(--line); border-radius:10px;
  padding:14px 16px; overflow-x:auto;
}
pre.sh .c { color:var(--faint); }
code {
  font-family:var(--mono); font-size:.9em; background:var(--panel);
  border:1px solid var(--line); border-radius:5px; padding:1px 5px;
}
kbd {
  font-family:var(--mono); font-size:.85em; background:var(--panel);
  border:1px solid var(--line); border-bottom-width:2px; border-radius:5px;
  padding:1px 6px; color:var(--accent);
}

/* ---- grids and tables ---- */
.cols { display:grid; grid-template-columns:repeat(auto-fit,minmax(250px,1fr)); gap:22px; margin-top:26px; }
.card { background:var(--panel); border:1px solid var(--line); border-radius:10px; padding:18px 20px; }
.card h4 { margin:0 0 7px; font-size:15px; }
.card p { margin:0; color:var(--dim); font-size:14.5px; max-width:none; }
table { border-collapse:collapse; width:100%; margin-top:22px; font-size:14.5px; }
th, td { text-align:left; padding:9px 12px; border-bottom:1px solid var(--line); vertical-align:top; }
th { color:var(--faint); font-weight:500; font-family:var(--mono); font-size:12px;
     letter-spacing:.06em; text-transform:uppercase; }
td:first-child { font-family:var(--mono); font-size:13px; white-space:nowrap; }
td code { background:none; border:none; padding:0; }

ul.plain { list-style:none; padding:0; margin:22px 0 0; max-width:74ch; }
ul.plain li { padding:9px 0 9px 26px; position:relative; color:var(--dim); border-bottom:1px solid var(--line); }
ul.plain li:last-child { border-bottom:none; }
ul.plain li::before { content:"—"; position:absolute; left:0; color:var(--accent); }
ul.plain li strong { color:var(--ink); font-weight:600; }

footer { border-top:1px solid var(--line); padding:38px 0 56px; color:var(--faint); font-size:14px; }
footer .row { display:flex; flex-wrap:wrap; gap:26px; align-items:baseline; }
footer .spacer { flex:1; }
@media (max-width:640px) {
  figure.frame pre { font-size:9.5px; }
  header { padding:44px 0 0; }
}
"""

BODY = f"""
<nav><div class="wrap">
  <img src="assets/braids-icon-64.png" alt="">
  <span class="name">braids</span>
  <span class="spacer"></span>
  <a class="link" href="#what">What it does</a>
  <a class="link" href="#local">Local</a>
  <a class="link" href="#commands">Commands</a>
  <a class="link" href="docs/">Docs</a>
  <a class="link" href="https://github.com/Ashes47/braids">GitHub</a>
</div></nav>

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
      <span id="cmd">brew install ashes47/tap/braids</span>
      <button class="copy" onclick="copyCmd(this)">copy</button>
    </div>
    <a class="btn primary" href="docs/">Read the docs</a>
    <a class="btn" href="https://github.com/Ashes47/braids">Star on GitHub</a>
  </div>
  <p class="note">
    Or <code>curl -fsSL https://braids.chat/install.sh | sh</code> without
    Homebrew. One binary, no daemon, no configuration file.
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

<section><div class="wrap">
  <h2><span class="kicker">Install</span>One binary</h2>
<pre class="sh"><span class="c"># homebrew</span>
brew install ashes47/tap/braids

<span class="c"># or without it — downloads the build for your machine and checks it</span>
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

<footer><div class="wrap"><div class="row">
  <span>braids — MIT licensed</span>
  <span class="spacer"></span>
  <a href="docs/">Docs</a>
  <a href="https://github.com/Ashes47/braids">GitHub</a>
  <a href="https://github.com/Ashes47/braids/blob/main/SPEC.md">Design notes</a>
  <a href="https://github.com/Ashes47/braids/issues">Issues</a>
</div></div></footer>

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

PAGE = f"""<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>braids — branch your Claude Code conversations</title>
<meta name="description" content="braids draws every Claude Code conversation and every branch as one graph, searches all of them in milliseconds, and turns any message into the start of a new conversation. Local, open source, nothing hosted.">
<link rel="icon" href="assets/braids-icon-64.png">
<meta property="og:title" content="braids — conversations don't go in a straight line">
<meta property="og:description" content="A terminal map of every Claude Code conversation and branch. Search all of them in milliseconds. Fork a new conversation from any message. Local, open source, nothing hosted.">
<meta property="og:image" content="assets/braids-icon-256.png">
<meta property="og:url" content="https://braids.chat">
<meta name="twitter:card" content="summary_large_image">
<style>{CSS}</style>
</head>
<body>{BODY}</body>
</html>
"""

(HERE / "index.html").write_text(PAGE)
print("wrote site/index.html —", len(PAGE), "bytes")

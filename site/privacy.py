#!/usr/bin/env python3
"""The privacy policy for braids.chat and braids.

Separate from docs/privacy, which explains to somebody using braids what it
reads and how to check. This is the other thing: a policy, at a stable URL, of
the kind a directory submission or a security review asks for.

It is short because the honest version is short. braids collects nothing, so
most of a privacy policy has nothing to describe.
"""

import pathlib

from chrome import CSS, footer, nav, page

HERE = pathlib.Path(__file__).parent

BODY = f"""{nav(active="")}
<div class="wrap" style="padding-top:48px; padding-bottom:40px; max-width:760px">
<h1 style="font-size:clamp(28px,3.6vw,40px)">Privacy</h1>
<p class="lede">
  braids collects nothing, sends nothing, and stores nothing anywhere but your
  own machine. What follows is that, said precisely.
</p>

<h2 id="software">The software</h2>
<p>
  braids reads the transcripts Claude Code has already written under
  <code>~/.claude</code>, and writes an index of them to
  <code>~/.braids</code>. Both stay on your computer. Nothing is uploaded,
  and there is nobody to upload it to: braids has no account, no server and no
  backend.
</p>
<p>
  <strong>It makes no network calls.</strong> The binary contains no HTTP
  client and opens no connections. You do not have to take that on trust:
</p>
<pre class="sh">go list -deps ./cmd/braids | grep net/http    <span class="c"># no output</span></pre>
<p>
  It never talks to a language model. braids arranges conversations you have
  with one; it does not have one of its own, and it needs no API key.
</p>
<p>
  Two things do use the network, both started by you and neither by braids.
  The installer at <code>braids.chat/install.sh</code> downloads a release from
  GitHub when you run it. And the map can offer to run that installer again
  when a build is old, which it decides by reading the binary's own
  modification time rather than by asking anything.
</p>

<h2 id="site">This website</h2>
<p>
  braids.chat is static files on GitHub Pages. There is no analytics script,
  no tag manager, no cookie, no font fetched from anywhere else and no content
  delivery network. Nothing on these pages tries to identify you.
</p>
<p>
  GitHub serves the files and keeps its own server logs, which is outside our
  control and covered by
  <a href="https://docs.github.com/site-policy/privacy-policies/github-privacy-statement">GitHub's
  privacy statement</a>.
</p>

<h2 id="plugin">The Claude Code plugin</h2>
<p>
  The plugin carries a skill and a set of hooks. The hooks run
  <code>braids hook</code> on your machine, which appends a line to
  <code>~/.braids/events.jsonl</code> recording that a session stopped or asked
  you something: the event name, the session id, the tool name and the working
  directory. No prompt, no response, no code and no tool output is recorded.
  The file never leaves your computer, and <code>braids hooks --remove</code>
  stops it being written.
</p>

<h2 id="data">What is collected</h2>
<p>
  Nothing. There is no telemetry, no crash reporting, no usage counting and no
  opt-out to offer, because there is nothing to opt out of. If that ever
  changes it will be opt-in, announced in the release notes, and this page
  will say so before it ships.
</p>

<h2 id="removing">Removing it</h2>
<pre class="sh">braids hooks --remove          <span class="c"># stop sessions reporting</span>
braids skill --remove          <span class="c"># take the skill back out</span>
rm -rf ~/.braids               <span class="c"># the index and its sidecar files</span>
rm "$(command -v braids)"      <span class="c"># the binary</span></pre>
<p>
  Your transcripts under <code>~/.claude</code> are Claude Code's, not braids',
  and removing braids leaves them exactly as they were. braids has only ever
  read them.
</p>

<h2 id="contact">Contact</h2>
<p>
  <a href="mailto:ashes4799@gmail.com">ashes4799@gmail.com</a>, or
  <a href="https://github.com/Ashes47/braids/issues">open an issue</a>. For
  anything security related, see
  <a href="https://github.com/Ashes47/braids/blob/main/SECURITY.md">SECURITY.md</a>.
</p>
<p class="note">Last updated 11 September 2026.</p>
</div>
{footer()}
"""


def build():
    out = HERE / "privacy"
    out.mkdir(parents=True, exist_ok=True)
    html = page(
        title="Privacy | braids",
        description="braids collects nothing, sends nothing, and stores nothing "
                    "anywhere but your own machine.",
        og_title="braids: privacy",
        body=BODY, css=CSS, path="/privacy/",
    )
    (out / "index.html").write_text(html)
    print(f"wrote privacy/index.html: {len(html) // 1024} KB")


if __name__ == "__main__":
    build()

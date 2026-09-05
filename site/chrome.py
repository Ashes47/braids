#!/usr/bin/env python3
"""Chrome shared by every page on braids.chat.

The nav, the footer and the stylesheet live here so the landing page and the
docs cannot drift apart — a docs page that styles its own header is a docs page
that looks a version behind the moment the header changes.

Links are written the way the landing page needs them, at the site root, and
retarget() rewrites them for a page that sits in a subdirectory. So every page
is served from the same tree, whether that is GitHub Pages or `make site`.
"""

import html
import pathlib
import re

HERE = pathlib.Path(__file__).parent
FRAMES = HERE / "frames"


def mark() -> str:
    """The ASCII mark for the page header, as `braids help` prints it.

    Read from the capture rather than written out here, so the page and
    internal/brand cannot disagree about the shape of the word.
    """
    lines = (FRAMES / "mark.txt").read_text().rstrip("\n").split("\n")
    art = "\n".join(html.escape(line) for line in lines[:-1])
    tag = html.escape(lines[-1])
    return f'<pre class="ascii" aria-hidden="true">{art}\n<span class="tag">{tag}</span></pre>'


def capture(name: str, cmd: str, caption: str = "") -> str:
    """A captured run of a command, as text rather than a picture.

    Command output that is plain text stays text: it is narrow enough to read
    at the page's own size, it can be copied, and it needs no image.
    """
    body = html.escape((FRAMES / f"{name}.txt").read_text().rstrip("\n"))
    cap = f'<figcaption>{caption}</figcaption>' if caption else ""
    return (f'<figure class="frame capture"><div class="shell">'
            f'<div class="cmdbar"><span class="prompt">$</span> <b>{cmd}</b></div>'
            f'<pre>{body}</pre></div>{cap}</figure>')


def frame(name: str, caption: str = "", cmd: str = "", then: str = "") -> str:
    """One captured braids screen, as an image.

    The picture is produced by `make frames`: real output against a fake
    ~/.claude, drawn from the capture by scripts/ansi2png.py. `cmd` and `then`
    label how you reach the screen, which is the first thing a reader wants and
    the one thing a screenshot cannot show, and they stay real text above the
    picture rather than being drawn into it.
    """
    alt = html.escape(caption or f"braids: the {name} screen")
    bar = ""
    if cmd:
        bar = (f'<div class="cmdbar"><span class="prompt">$</span> <b>{cmd}</b>'
               + (f'<span class="then">then {then}</span>' if then else "")
               + "</div>")
    cap = f'<figcaption>{caption}</figcaption>' if caption else ""
    return (f'<figure class="frame"><div class="shell">{bar}'
            f'<a class="full" href="/assets/frames/{name}.png" '
            f'title="Open this screen at full size">'
            f'<img src="/assets/frames/{name}.png" alt="{alt}" loading="lazy"></a>'
            f'</div>{cap}</figure>')


def retarget(markup: str, home: str) -> str:
    """Point root-relative links and in-page anchors at `home`.

    The chrome is authored for the landing page, where an anchor is just
    `#local`. From inside docs/ that anchor has to become `/#local`, and the
    logo has to become `/assets/...`. Everything else is left alone, so an
    absolute URL to GitHub survives untouched.
    """
    if not home:
        return markup
    markup = re.sub(r'(href|src)="(?=#|assets/|docs/|install\.sh)', rf'\1="{home}', markup)
    return markup


NAV = """<nav><div class="wrap">
  <a class="brand" href="#">
    <img src="assets/braids-icon-64.png" alt="">
    <span class="name">braids</span>
  </a>
  <span class="spacer"></span>
  <div class="links">
    <a class="link" href="#find">Find</a>
    <a class="link" href="#explain">Explain</a>
    <a class="link" href="#branch">Branch</a>
    <a class="link" href="#map">Map</a>
    <a class="link" href="#install">Install</a>
    <a class="link" href="docs/">Docs</a>
    <a class="link" href="https://github.com/Ashes47/braids">GitHub</a>
    <a class="link" href="#star">★ Star</a>
  </div>
</div></nav>"""

FOOTER = """<footer><div class="wrap">
  <div class="cols footcols">
    <div>
      <h5>braids</h5>
      <a href="#find">Find</a>
      <a href="#explain">Explain</a>
      <a href="#branch">Branch</a>
      <a href="#local">Local</a>
    </div>
    <div>
      <h5>Read</h5>
      <a href="docs/">Documentation</a>
      <a href="https://github.com/Ashes47/braids/blob/main/SPEC.md">Design notes</a>
      <a href="https://github.com/Ashes47/braids/blob/main/CONTRIBUTING.md">Contributing</a>
      <a href="https://github.com/Ashes47/braids/blob/main/SECURITY.md">Security</a>
    </div>
    <div>
      <h5>Code</h5>
      <a href="https://github.com/Ashes47/braids">Repository</a>
      <a href="https://github.com/Ashes47/braids/issues">Issues</a>
      <a href="https://github.com/Ashes47/braids/releases">Releases</a>
      <a href="https://braids.chat/install.sh">install.sh</a>
    </div>
  </div>
  <div class="row bottom">
    <span>MIT licensed</span>
    <span class="icons">
      <a href="https://x.com/devanujhere" title="@devanujhere on X" aria-label="X">
        <svg viewBox="0 0 24 24" width="15" height="15" fill="currentColor" aria-hidden="true">
          <path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-5.214-6.817L4.99 21.75H1.68l7.73-8.835L1.254 2.25H8.08l4.713 6.231zm-1.161 17.52h1.833L7.084 4.126H5.117z"/>
        </svg>
      </a>
      <a href="mailto:ashes4799@gmail.com" title="ashes4799@gmail.com" aria-label="Email">
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor"
             stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <rect x="2.5" y="4.5" width="19" height="15" rx="2.5"/>
          <path d="M3 6.5l9 6.5 9-6.5"/>
        </svg>
      </a>
    </span>
    <span class="spacer"></span>
    <span>braids never talks to a model. It arranges the conversations you have with one.</span>
  </div>
</div></footer>"""


def nav(home: str = "", active: str = "") -> str:
    """The sticky top bar. `active` is the href of the link to mark as current."""
    out = retarget(NAV, home)
    if active:
        out = out.replace(f'<a class="link" href="{active}"',
                          f'<a class="link on" href="{active}"', 1)
    return out


def footer(home: str = "") -> str:
    return retarget(FOOTER, home)


def scrollableTables(markup: str) -> str:
    """Put every table in a container that can scroll sideways.

    A table of commands does not fit a phone and must not be squeezed into
    one: the first column is a command, and a command broken across lines is
    a command you cannot read. So the table keeps its natural width and the
    container scrolls. Done here rather than at each call site, because a
    table written later would otherwise quietly overflow the page.
    """
    markup = markup.replace("<table>", '<div class="tablewrap"><table>')
    markup = re.sub(r'<table class="([^"]*)">', r'<div class="tablewrap"><table class="\1">', markup)
    return markup.replace("</table>", "</table></div>")


def page(*, title: str, description: str, body: str, css: str,
         og_title: str = "", og_description: str = "", home: str = "") -> str:
    """One complete, self-contained HTML page.

    Self-contained matters: the stylesheet is inlined and there is no font, no
    analytics script and no CDN, so reading about a tool that never phones home
    does not phone home either.
    """
    a = home + "assets"
    body = scrollableTables(body)
    return f"""<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{title}</title>
<meta name="description" content="{description}">
<link rel="icon" href="{a}/braids-icon-64.png">
<meta property="og:title" content="{og_title or title}">
<meta property="og:description" content="{og_description or description}">
<meta property="og:image" content="{a}/braids-icon-256.png">
<meta property="og:url" content="https://braids.chat">
<meta name="twitter:card" content="summary_large_image">
<style>{css}</style>
</head>
<body>{body}</body>
</html>
"""


def _frame_columns() -> int:
    """How wide the captured frames are, in columns."""
    widest = 0
    for f in FRAMES.glob("*.ans"):
        for line in f.read_text().split("\n"):
            widest = max(widest, len(SGR.sub("", line)))
    return widest or 138


CSS = r"""
:root {
  --bg:#0b0d10; --panel:#11141a; --line:#232833; --ink:#e6edf3; --dim:#8b949e;
  --faint:#6e7681; --accent:#f0883e; --blue:#3b93f7; --ok:#3fb950;
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
nav .brand img { height:26px; width:auto; display:block; }
nav img { height:26px; width:auto; display:block; }
nav .brand { display:flex; align-items:center; gap:10px; }
nav .brand:hover { text-decoration:none; }
nav .brand:hover .name { color:var(--accent); }
nav .name { font-family:var(--mono); font-weight:600; letter-spacing:.02em; color:var(--ink);
            transition:color .12s ease; }
nav .spacer { flex:1; }
nav .links { display:flex; align-items:center; gap:20px; min-width:0; }
nav a.link { color:var(--dim); font-size:14px; }
nav a.link:hover { color:var(--ink); text-decoration:none; }
nav a.link.on { color:var(--accent); }

/* ---- hero ---- */
header { padding:72px 0 8px; }
header .marks { display:flex; align-items:center; gap:26px; margin-bottom:28px; }
header .mark { height:92px; width:auto; }
header pre.ascii {
  font-family:var(--mono); font-size:clamp(6px,.92vw,11px); line-height:1.25;
  margin:0; color:var(--accent); white-space:pre;
}
header pre.ascii .tag { color:var(--dim); }

/* A section that has a page of its own says so, once, at the end of it. */
p.more { margin:18px 0 0; font-size:14.5px; }
p.more a { font-weight:600; }
p.more a::after { content:" \2192"; }
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

/* ---- terminal frames ----
   A frame is a picture of a terminal, not a block of text pretending to be
   one. It is captured at 183 columns, which is where every screen draws its
   facts, its glyph key, all its bindings and the full mark, and then drawn at
   whatever width the column gives it. Rendered at 26px a character and
   resampled down, it stays readable at a width where live text would be an 8px
   font, and it lines up with the prose because it is simply in the column.

   This replaced a font-size derived from the frame's own width through a
   container query, with a viewport fallback, a cap and a mobile override. That
   arithmetic produced three separate bugs: a floor that pushed the panel
   border out of view, a fallback that disagreed with the container inside the
   docs layout, and a placeholder left in a comment. None of it is needed to
   put a picture in a column. */
figure.frame { margin:26px 0 0; }
figure.frame .shell {
  background:var(--panel); border:1px solid var(--line); border-radius:11px;
  overflow:hidden;
}
figure.frame .cmdbar {
  display:flex; align-items:center; gap:9px; flex-wrap:wrap;
  font-family:var(--mono); font-size:12px; color:var(--dim);
  padding:9px 15px; border-bottom:1px solid var(--line);
  background:rgba(255,255,255,.016);
}
figure.frame .cmdbar .prompt { color:var(--faint); }
figure.frame .cmdbar b { color:var(--ink); font-weight:600; }
figure.frame .cmdbar .then { color:var(--faint); }
figure.frame .cmdbar kbd { font-size:11px; padding:0 5px; }
figure.frame a.full { display:block; }
figure.frame a.full:hover { text-decoration:none; }
figure.frame img { display:block; width:100%; height:auto; }
/* A text capture is narrow enough to read at the page's own size. */
figure.frame.capture pre {
  font-family:var(--mono); font-size:12.5px; line-height:1.55; margin:0;
  padding:16px 18px; overflow-x:auto; color:var(--dim); white-space:pre;
}
figure.frame.capture pre { -webkit-overflow-scrolling:touch; }
figure.frame figcaption { color:var(--faint); font-size:13px; margin-top:10px; text-align:center; }

pre.sh {
  font-family:var(--mono); font-size:13px; line-height:1.7; margin:16px 0 0;
  background:var(--panel); border:1px solid var(--line); border-radius:10px;
  padding:14px 16px; overflow-x:auto;
}
pre.sh .c { color:var(--faint); }
code {
  font-family:var(--mono); font-size:.9em; background:var(--panel);
  border:1px solid var(--line); border-radius:5px; padding:1px 5px;
  /* A path written inline is one long word. On a narrow screen it has to be
     allowed to break, or it carries the whole page sideways with it; anywhere
     only breaks a word that would not otherwise fit, so wider screens are
     left as they were. */
  overflow-wrap:anywhere;
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
.card h4 a { color:var(--ink); }
.card h4 a::after { content:" \2192"; color:var(--accent); opacity:0; transition:opacity .12s ease; }
.card:hover { border-color:var(--faint); }
.card:hover h4 a { color:var(--accent); text-decoration:none; }
.card:hover h4 a::after { opacity:1; }
.card p { margin:0; color:var(--dim); font-size:14.5px; max-width:none; }
.tablewrap { overflow-x:auto; -webkit-overflow-scrolling:touch; margin-top:22px; }
table { border-collapse:collapse; width:100%; font-size:14.5px; }
th, td { text-align:left; padding:9px 12px; border-bottom:1px solid var(--line); vertical-align:top; }
th { color:var(--faint); font-weight:500; font-family:var(--mono); font-size:12px;
     letter-spacing:.06em; text-transform:uppercase; }
td:first-child { font-family:var(--mono); font-size:13px; white-space:nowrap; }
td code { background:none; border:none; padding:0; }

ul.plain { list-style:none; padding:0; margin:22px 0 0; max-width:74ch; }
ul.plain li { padding:9px 0 9px 26px; position:relative; color:var(--dim); border-bottom:1px solid var(--line); }
ul.plain li:last-child { border-bottom:none; }
ul.plain li::before { content:"\203A"; position:absolute; left:2px; color:var(--accent);
                      font-weight:600; }
ul.plain li strong { color:var(--ink); font-weight:600; }

footer { border-top:1px solid var(--line); padding:42px 0 56px; color:var(--faint); font-size:14px; }
footer .row { display:flex; flex-wrap:wrap; gap:26px; align-items:baseline; }
footer .spacer { flex:1; }
footer .footcols { margin-top:0; gap:26px; }
footer .footcols > div { display:flex; flex-direction:column; gap:7px; }
footer h5 {
  margin:0 0 5px; font-family:var(--mono); font-size:11.5px; font-weight:500;
  letter-spacing:.09em; text-transform:uppercase; color:var(--dim);
}
footer .footcols a { color:var(--faint); }
footer .footcols a:hover { color:var(--ink); text-decoration:none; }
footer .bottom { margin-top:34px; padding-top:20px; border-top:1px solid var(--line); font-size:13px;
                 align-items:center; gap:18px; }
footer .icons { display:inline-flex; align-items:center; gap:14px; }
footer .icons a {
  color:var(--faint); display:inline-flex; align-items:center;
  transition:color .12s ease;
}
footer .icons a:hover { color:var(--ink); text-decoration:none; }
/* ---- narrow screens ----
   These rules answer measurements rather than guesses. scripts/responsive.py
   loads every page in a frame 390 CSS pixels wide and reports any element
   whose box crosses the edge of it. Before this block two things did, on
   every page: the nav laid eight links in a row that could not wrap, and the
   tables held a first column set never to break. Nothing else did, which is
   why nothing else is touched here. */
@media (max-width:760px) {
  .wrap { padding:0 18px; }

  /* The nav keeps all eight links and lets them scroll, rather than hiding
     some behind a button, which would need script on a page that has none.
     The brand stays put because it is outside the scrolling group. */
  nav .wrap { gap:12px; height:54px; }
  nav .spacer { display:none; }
  nav .links {
    flex:1; gap:17px; overflow-x:auto; scrollbar-width:none;
    -webkit-overflow-scrolling:touch;
  }
  nav .links::-webkit-scrollbar { display:none; }
  nav a.link { white-space:nowrap; }

  header { padding:40px 0 0; }
  header .marks { gap:18px; margin-bottom:22px; }
  header .mark { height:68px; }

  section { padding:40px 0; }
  /* The command is the one thing on the page a reader has to be able to copy,
     so it wraps onto a second line rather than running off the screen. */
  .install { flex-wrap:wrap; font-size:12.5px; padding:10px 12px; }
  .cta { gap:10px; margin:22px 0 6px; }
  .btn { padding:10px 14px; }
  ul.plain li { padding-left:22px; }
}

@media (max-width:640px) {
  figure.frame pre { font-size:9.5px; }
  header pre.ascii { display:none; }
}
"""

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


SGR = re.compile(r"\x1b\[([0-9;]*)m")


def _xterm(n: int) -> str:
    """One of the 256 palette colours, as a hex string."""
    if n < 16:
        base = [(0, 0, 0), (205, 0, 0), (0, 205, 0), (205, 205, 0), (0, 0, 238),
                (205, 0, 205), (0, 205, 205), (229, 229, 229), (127, 127, 127),
                (255, 0, 0), (0, 255, 0), (255, 255, 0), (92, 92, 255),
                (255, 0, 255), (0, 255, 255), (255, 255, 255)][n]
        return "#%02x%02x%02x" % base
    if n < 232:
        n -= 16
        steps = [0, 95, 135, 175, 215, 255]
        return "#%02x%02x%02x" % (steps[n // 36], steps[n % 36 // 6], steps[n % 6])
    grey = 8 + 10 * (n - 232)
    return "#%02x%02x%02x" % (grey, grey, grey)


def ansi_to_html(text: str) -> str:
    """Turn a captured terminal frame into HTML, colours and all.

    braids writes truecolor even into a pipe, so a screenshot keeps the exact
    palette the program chose. Rendering those bytes rather than a stripped
    copy is the difference between showing the product and describing it.

    Only the sequences braids emits are handled: reset, bold, faint, and
    24-bit or 256-colour foreground and background. Anything else is dropped
    rather than guessed at, so an unrecognised sequence loses a colour instead
    of leaking escape codes into the page.
    """
    out, style, pos = [], {}, 0

    def push(chunk: str) -> None:
        if not chunk:
            return
        css = ""
        if "fg" in style:
            css += f"color:{style['fg']};"
        if "bg" in style:
            css += f"background:{style['bg']};"
        if style.get("bold"):
            css += "font-weight:600;"
        if style.get("faint"):
            css += "opacity:.62;"
        if css:
            out.append(f'<span style="{css}">{html.escape(chunk)}</span>')
        else:
            out.append(html.escape(chunk))

    for m in SGR.finditer(text):
        push(text[pos:m.start()])
        pos = m.end()
        params = [int(p) for p in m.group(1).split(";") if p != ""] or [0]
        i = 0
        while i < len(params):
            p = params[i]
            if p == 0:
                style = {}
            elif p == 1:
                style["bold"] = True
            elif p == 2:
                style["faint"] = True
            elif p == 22:
                style.pop("bold", None)
                style.pop("faint", None)
            elif p == 39:
                style.pop("fg", None)
            elif p == 49:
                style.pop("bg", None)
            elif p in (38, 48) and i + 1 < len(params):
                where = "fg" if p == 38 else "bg"
                if params[i + 1] == 2 and i + 4 < len(params):
                    r, g, b = params[i + 2:i + 5]
                    style[where] = "#%02x%02x%02x" % (r, g, b)
                    i += 4
                elif params[i + 1] == 5 and i + 2 < len(params):
                    style[where] = _xterm(params[i + 2])
                    i += 2
            i += 1
    push(text[pos:])
    return "".join(out)


def frame(name: str, caption: str = "", cmd: str = "", then: str = "") -> str:
    """A verbatim braids screenshot, as produced by scripts/demo.py.

    The .ans capture keeps braids' colours; .txt is the same frame with the
    escapes removed, for anywhere they would show up as noise. `cmd` and `then`
    label how you reach the screen, which is the first thing a reader wants to
    know and the one thing a screenshot cannot show.
    """
    coloured = FRAMES / f"{name}.ans"
    if coloured.exists():
        body = ansi_to_html(coloured.read_text().rstrip("\n"))
    else:
        body = html.escape((FRAMES / f"{name}.txt").read_text().rstrip("\n"))
    bar = ""
    if cmd:
        bar = (f'<div class="cmdbar"><span class="prompt">$</span> <b>{cmd}</b>'
               + (f'<span class="then">then {then}</span>' if then else "")
               + "</div>")
    cap = f'<figcaption>{caption}</figcaption>' if caption else ""
    return (f'<figure class="frame"><div class="shell">{bar}'
            f'<pre>{body}</pre></div>{cap}</figure>')


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
  <a class="link" href="#what">What it does</a>
  <a class="link" href="#work">Screens</a>
  <a class="link" href="#local">Local</a>
  <a class="link" href="#commands">Commands</a>
  <a class="link" href="docs/">Docs</a>
  <a class="link" href="https://github.com/Ashes47/braids">GitHub</a>
  <a class="link" href="#star">★ Star</a>
</div></nav>"""

FOOTER = """<footer><div class="wrap">
  <div class="cols footcols">
    <div>
      <h5>braids</h5>
      <a href="#what">What it does</a>
      <a href="#local">Local</a>
      <a href="#commands">Commands</a>
      <a href="#star">Star it</a>
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


def page(*, title: str, description: str, body: str, css: str,
         og_title: str = "", og_description: str = "", home: str = "") -> str:
    """One complete, self-contained HTML page.

    Self-contained matters: the stylesheet is inlined and there is no font, no
    analytics script and no CDN, so reading about a tool that never phones home
    does not phone home either.
    """
    a = home + "assets"
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


CSS = """
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
nav a.link { color:var(--dim); font-size:14px; }
nav a.link:hover { color:var(--ink); text-decoration:none; }
nav a.link.on { color:var(--accent); }

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

/* ---- terminal frames ----
   A frame is 195 columns of real terminal, which is wider than the prose
   column. It breaks out to the width of the window and takes a font size that
   fits those columns on a desktop, because a screenshot you have to scroll
   sideways is a screenshot nobody reads. Narrow windows still scroll, at a
   size you can actually see. */
figure.frame {
  margin:26px 0 0; width:min(1400px, calc(100vw - 40px));
  margin-left:50%; transform:translateX(-50%);
  /* So the size above is worked out from this box rather than the window. */
  container-type:inline-size;
}
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
figure.frame pre {
  font-family:var(--mono); margin:0;
  padding:15px 16px; overflow-x:auto; color:var(--ink);
  -webkit-overflow-scrolling:touch;
  /* A terminal has no gap between its rows, and braids draws boxes. At 1.5
     the vertical rules stop touching and every panel border reads as a dashed
     line, which is what "the screens look misaligned" turned out to mean. */
  line-height:1.15;
  /* A frame is 195 columns and has to fit its box, or the panel border
     scrolls off the right. One column is 0.6em in every face in --mono, so
     the size comes from the width the figure actually gets. The first value
     is the fallback for browsers without container queries; the second is
     exact, because 100cqw is this figure, which inside the docs layout is not
     the window. */
  font-size:min(11.5px, calc((min(1400px, 100vw - 40px) - 34px) / 121));
  font-size:min(11.5px, calc((100cqw - 34px) / 121));
}
@media (max-width:700px) {
  /* On a phone 195 columns cannot both fit and be legible. Pick legible and
     let it scroll sideways. */
  figure.frame pre { font-size:8px; }
}
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
ul.plain li::before { content:"\\203A"; position:absolute; left:2px; color:var(--accent);
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
@media (max-width:640px) {
  figure.frame pre { font-size:9.5px; }
  header { padding:44px 0 0; }
}
"""

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
/* ---- docs layout ---- */
.docs { display:grid; grid-template-columns:216px minmax(0,1fr); gap:44px; padding:38px 0 72px; }
.side { position:sticky; top:78px; align-self:start; max-height:calc(100vh - 100px); overflow-y:auto; }
.side h6 {
  margin:0 0 8px; font-family:var(--mono); font-size:11px; font-weight:500;
  letter-spacing:.09em; text-transform:uppercase; color:var(--faint);
}
.side nav { position:static; background:none; border:none; backdrop-filter:none; }
.side a { display:block; color:var(--dim); font-size:14.5px; padding:4px 0 4px 11px;
          border-left:1px solid var(--line); }
.side a:hover { color:var(--ink); text-decoration:none; border-left-color:var(--faint); }
.side a.on { color:var(--accent); border-left-color:var(--accent); }
.side .group { margin-bottom:24px; }
.side .onthis a { font-size:13.5px; color:var(--faint); }
.side .onthis a:hover { color:var(--ink); }

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

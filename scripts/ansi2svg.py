#!/usr/bin/env python3
"""Turn a captured braids frame into an SVG.

GitHub will not render ANSI, so a README either shows a terminal in black and
white or shows a picture of one. This draws the picture from the same .ans
capture the site renders, so the two cannot disagree.

Every colour run is placed at an absolute x and pinned with textLength, so the
columns line up even where the reader's monospace font has a different advance
than the one this assumed.

    python3 scripts/ansi2svg.py site/frames/map.ans assets/frames/map.svg
"""

import pathlib
import re
import sys
import xml.sax.saxutils as sax

SGR = re.compile(r"\x1b\[([0-9;]*)m")

FONT = ('ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, '
        '"DejaVu Sans Mono", monospace')
SIZE = 13.5          # font size in px
ADVANCE = SIZE * 0.6  # one column
LINE = SIZE * 1.5     # one row
PAD = 15
INK = "#e6edf3"       # braids' default foreground
PANEL = "#11141a"     # the site's panel colour, so the two match
EDGE = "#232833"


def runs(line: str):
    """Split one captured line into (column, text, style) runs."""
    out, style, pos, col = [], {}, 0, 0
    for m in SGR.finditer(line):
        text = line[pos:m.start()]
        if text:
            out.append((col, text, dict(style)))
            col += len(text)
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
            elif p in (38, 48) and i + 4 < len(params) and params[i + 1] == 2:
                r, g, b = params[i + 2:i + 5]
                style["fg" if p == 38 else "bg"] = "#%02x%02x%02x" % (r, g, b)
                i += 4
            i += 1
    tail = line[pos:]
    if tail:
        out.append((col, tail, dict(style)))
    return out


def convert(ans: str) -> str:
    lines = ans.rstrip("\n").split("\n")
    cols = max((len(SGR.sub("", line)) for line in lines), default=0)
    width = round(cols * ADVANCE + 2 * PAD)
    height = round(len(lines) * LINE + 2 * PAD)

    body = []
    for row, line in enumerate(lines):
        y = round(PAD + (row + 0.8) * LINE, 2)
        spans = []
        for col, text, style in runs(line):
            if not text.strip():
                continue
            attrs = [f'x="{round(PAD + col * ADVANCE, 2)}"',
                     f'textLength="{round(len(text) * ADVANCE, 2)}"',
                     'lengthAdjust="spacing"']
            fill = style.get("fg", INK)
            attrs.append(f'fill="{fill}"')
            if style.get("bold"):
                attrs.append('font-weight="600"')
            if style.get("faint"):
                attrs.append('opacity="0.62"')
            spans.append(f'<tspan {" ".join(attrs)}>{sax.escape(text)}</tspan>')
        if spans:
            body.append(f'<text y="{y}">{"".join(spans)}</text>')

    return (
        f'<svg xmlns="http://www.w3.org/2000/svg" width="{width}" height="{height}" '
        f'viewBox="0 0 {width} {height}" font-family=\'{FONT}\' '
        f'font-size="{SIZE}" xml:space="preserve">\n'
        f'<rect width="{width}" height="{height}" rx="10" fill="{PANEL}" '
        f'stroke="{EDGE}"/>\n'
        + "\n".join(body)
        + "\n</svg>\n"
    )


def main() -> None:
    if len(sys.argv) != 3:
        sys.exit("usage: ansi2svg.py FRAME.ans OUT.svg")
    src, dst = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2])
    dst.parent.mkdir(parents=True, exist_ok=True)
    svg = convert(src.read_text())
    dst.write_text(svg)
    print(f"{dst}: {len(svg) // 1024} KB")


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""Turn a captured braids frame into a PNG.

GitHub will not render ANSI, so a README either shows a terminal in black and
white or shows a picture of one. This draws the picture from the same .ans
capture the site renders, with braids' own colours.

Text is drawn run by run at a column position worked out here rather than left
to a text layout engine, so the box drawing lines up exactly. Menlo carries
every glyph a frame uses, box drawing and the warning sign included, so nothing
falls back to a missing-glyph box.

    python3 scripts/ansi2png.py site/frames/map.ans assets/frames/map.png
"""

import pathlib
import re
import sys

from PIL import Image, ImageDraw, ImageFont

SGR = re.compile(r"\x1b\[([0-9;]*)m")

# Menlo, because it is on every mac, is a true monospace, and covers the box
# drawing. Index 0 is Regular and 1 is Bold in the collection.
FACE = "/System/Library/Fonts/Menlo.ttc"
SIZE = 26          # drawn at twice the size it is shown at, for retina
PAD = 30
RADIUS = 20
INK = (230, 237, 243)     # braids' default foreground
PANEL = (17, 20, 26)      # the site's panel colour, so the two match
EDGE = (35, 40, 51)


def styled_runs(line: str):
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
                style["fg" if p == 38 else "bg"] = tuple(params[i + 2:i + 5])
                i += 4
            i += 1
    tail = line[pos:]
    if tail:
        out.append((col, tail, dict(style)))
    return out


def blend(colour, ground, amount):
    return tuple(round(c * amount + g * (1 - amount)) for c, g in zip(colour, ground))


def convert(ans: str) -> Image.Image:
    lines = ans.rstrip("\n").split("\n")
    regular = ImageFont.truetype(FACE, SIZE, index=0)
    bold = ImageFont.truetype(FACE, SIZE, index=1)
    advance = regular.getlength("M")
    step = round(SIZE * 1.5)

    cols = max((len(SGR.sub("", line)) for line in lines), default=0)
    width = round(cols * advance + 2 * PAD)
    height = len(lines) * step + 2 * PAD

    img = Image.new("RGB", (width, height), PANEL)
    draw = ImageDraw.Draw(img)
    draw.rounded_rectangle([0, 0, width - 1, height - 1], RADIUS,
                           fill=PANEL, outline=EDGE, width=2)

    for row, line in enumerate(lines):
        y = PAD + row * step
        for col, text, style in styled_runs(line):
            x = PAD + col * advance
            ground = style.get("bg", PANEL)
            if "bg" in style:
                draw.rectangle([x, y, x + len(text) * advance, y + step], fill=ground)
            if not text.strip():
                continue
            colour = style.get("fg", INK)
            if style.get("faint"):
                colour = blend(colour, ground, 0.62)
            draw.text((x, y), text, font=bold if style.get("bold") else regular,
                      fill=colour)
    return img


def main() -> None:
    if len(sys.argv) != 3:
        sys.exit("usage: ansi2png.py FRAME.ans OUT.png")
    src, dst = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2])
    dst.parent.mkdir(parents=True, exist_ok=True)
    img = convert(src.read_text())
    img.save(dst, optimize=True)
    print(f"{dst}: {img.width}x{img.height}, {dst.stat().st_size // 1024} KB")


if __name__ == "__main__":
    main()

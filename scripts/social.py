#!/usr/bin/env python3
"""Draw the card that shows up when a braids link is shared.

Slack, X, Discord, iMessage and every link unfurler want one landscape image
at a fixed shape, and they want it at an absolute URL. What they were given
was the 256px app icon at a relative one, which is two reasons for a link to
braids.chat to unfurl as a bare grey rectangle.

The card is the map, because the map is what braids is: one screen showing
every conversation on the machine, nested under the conversation each was cut
from. It is the same capture the site and the README use, so the picture in
the unfurl is the program's real output rather than a drawing of it.

    python3 scripts/social.py            # writes assets/social.png

Needs Pillow and Menlo, the same as the frames. It is not part of `make ci`
for that reason; `make frames` recaptures the map and then redraws this.
"""

import pathlib
import sys

from PIL import Image, ImageDraw, ImageFont

HERE = pathlib.Path(__file__).resolve().parent
ROOT = HERE.parent
MAP = ROOT / "assets" / "frames" / "map.png"
LOGO = ROOT / "assets" / "braids-icon-256.png"
OUT = ROOT / "assets" / "social.png"

# The shape every unfurler crops to. Anything else gets cut somewhere
# unflattering, usually through the middle of a word.
WIDTH, HEIGHT = 1200, 630

# The site's own colours, so the card and the page it opens agree.
BG = (11, 13, 16)
INK = (230, 237, 243)
DIM = (139, 148, 158)
ACCENT = (240, 136, 62)
LINE = (35, 40, 51)

FACE = "/System/Library/Fonts/Menlo.ttc"
SANS = "/System/Library/Fonts/Supplemental/Arial.ttf"


def font(path: str, size: int, index: int = 0) -> ImageFont.FreeTypeFont:
    try:
        return ImageFont.truetype(path, size, index=index)
    except OSError:
        return ImageFont.load_default(size)


def main() -> int:
    if not MAP.exists():
        print(f"{MAP} is missing. Run: make frames", file=sys.stderr)
        return 2

    card = Image.new("RGB", (WIDTH, HEIGHT), BG)
    draw = ImageDraw.Draw(card)

    # The wordmark, with the icon beside it.
    logo = Image.open(LOGO).convert("RGBA").resize((50, 50), Image.LANCZOS)
    card.paste(logo, (56, 42), logo)
    draw.text((120, 47), "braids", font=font(FACE, 38, index=1), fill=INK)

    # What it is, in one line, in the words the site uses.
    draw.text((57, 108), "Claude Code runs one thread. Your work runs twelve.",
              font=font(SANS, 26), fill=DIM)

    # The map itself. It is a very wide, short picture and the card is neither,
    # so it takes the width it can have and the space left under it goes to
    # words rather than to more empty background.
    shot = Image.open(MAP).convert("RGB")
    width = WIDTH - 112
    scaled = shot.resize((width, width * shot.height // shot.width), Image.LANCZOS)

    top = 162
    draw.rectangle((55, top - 1, 56 + scaled.width, top + scaled.height), outline=LINE)
    card.paste(scaled, (56, top))

    # Three lines for the three things it does, which is what somebody seeing
    # the link wants to know and cannot read off a screenshot this small.
    y = top + scaled.height + 44
    for label, rest in (
        ("Search", "every session on your machine, in milliseconds"),
        ("Explain", "which conversation produced a file, against the git log"),
        ("Branch", "a new conversation from any turn of an old one"),
    ):
        draw.text((57, y), label, font=font(FACE, 22, index=1), fill=ACCENT)
        draw.text((57 + 118, y), rest, font=font(SANS, 22), fill=DIM)
        y += 40

    # And the one claim worth making in an unfurl.
    draw.text((57, HEIGHT - 46), "local  ·  open source  ·  never talks to a model",
              font=font(FACE, 19), fill=ACCENT)

    card.save(OUT, "PNG", optimize=True)
    print(f"wrote {OUT.relative_to(ROOT)}: {OUT.stat().st_size // 1024} KB, {WIDTH}x{HEIGHT}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

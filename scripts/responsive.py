#!/usr/bin/env python3
"""Check that no page on braids.chat runs off the side of a phone.

Responsive is a claim that can be measured, so it is measured here rather than
eyeballed. Every page is loaded in a frame of a chosen width and every element
in it is asked where its box ends. Anything reaching past the edge is reported
with the width it reached, which is usually enough to name the rule at fault.

Two things are deliberately not counted. An element inside a container that
scrolls sideways is meant to stick out, so a wide table in its scroller and a
long command in its pre are both fine. And only the outermost offender in a
chain is named, because a table that overflows drags every cell with it and
listing all of them buries the one that matters.

    python3 scripts/responsive.py             # the usual phone widths
    python3 scripts/responsive.py 320 768     # widths of your choosing

Needs Chrome, and says so and stops rather than failing obscurely if it is
missing. Nothing here touches the network or the built site: the pages are
served from a temporary local server, and the harness that measures them is
synthesised in memory.
"""

import functools
import http.server
import pathlib
import shutil
import socket
import subprocess
import sys
import threading

HERE = pathlib.Path(__file__).resolve().parent
SITE = HERE.parent / "site"

# The widths worth checking: the narrowest phone still in use, the common
# Android and iPhone sizes, and a tablet held upright.
WIDTHS = [320, 360, 390, 414, 768]

CHROMES = [
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    "/Applications/Chromium.app/Contents/MacOS/Chromium",
    "google-chrome",
    "chromium",
    "chromium-browser",
]

# The harness. It puts one page in a frame of the width being tested, which
# gives that page a viewport of exactly that width and makes its media queries
# behave as they would on a device. Then it walks the frame and reports.
HARNESS = """<!doctype html><title>PENDING</title><body style="margin:0">
<script>
var q = new URLSearchParams(location.search);
var f = document.createElement('iframe');
f.style.cssText = 'width:' + (q.get('w') || '390') + 'px; height:1200px; border:0';
f.src = q.get('u') || '/';
document.body.appendChild(f);
f.onload = function () {
  setTimeout(function () {
    var d = f.contentDocument, de = d.documentElement, lim = de.clientWidth + 1, bad = [];
    d.querySelectorAll('body *').forEach(function (e) {
      var r = e.getBoundingClientRect();
      if (r.width === 0 || r.height === 0) return;
      if (r.right <= lim && r.left >= -1) return;
      for (var p = e.parentElement; p; p = p.parentElement) {
        var ox = f.contentWindow.getComputedStyle(p).overflowX;
        if (ox === 'auto' || ox === 'scroll' || ox === 'hidden') return;
        var pr = p.getBoundingClientRect();
        if (pr.right > lim || pr.left < -1) return;
      }
      var cls = (e.className && typeof e.className === 'string')
        ? '.' + e.className.trim().split(/\\s+/).join('.') : '';
      bad.push(e.tagName.toLowerCase() + cls +
               ' reaches ' + Math.round(r.right) + 'px');
    });
    document.title = 'PROBE scroll=' + de.scrollWidth +
      ' over=' + (bad.length ? bad.slice(0, 6).join(' ; ') : 'none');
  }, 400);
};
</script>"""


def pages() -> list[str]:
    """Every page in the built site, the landing page first."""
    found = ["/"]
    for f in sorted(SITE.glob("docs/**/index.html")):
        found.append("/" + str(f.parent.relative_to(SITE)).replace("\\", "/") + "/")
    return found


def chrome() -> str | None:
    for c in CHROMES:
        if pathlib.Path(c).exists() or shutil.which(c):
            return c
    return None


class Handler(http.server.SimpleHTTPRequestHandler):
    """The built site, plus the harness, which exists only in memory."""

    def do_GET(self):  # noqa: N802 - the name is the base class's
        if self.path.startswith("/__probe"):
            body = HARNESS.encode()
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        super().do_GET()

    def log_message(self, *_):
        pass


def serve() -> tuple[http.server.HTTPServer, int]:
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        port = s.getsockname()[1]
    handler = functools.partial(Handler, directory=str(SITE))
    server = http.server.HTTPServer(("127.0.0.1", port), handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    return server, port


def measure(browser: str, port: int, page: str, width: int) -> str:
    url = f"http://127.0.0.1:{port}/__probe.html?w={width}&u={page}"
    out = subprocess.run(
        [browser, "--headless", "--disable-gpu", "--no-sandbox",
         "--window-size=1400,1300", "--virtual-time-budget=6000",
         "--dump-dom", url],
        capture_output=True, text=True, timeout=120).stdout
    for line in out.split("<"):
        if line.startswith("title>PROBE"):
            return line[len("title>"):]
    return "NO RESULT (the page did not report)"


def main() -> int:
    if not (SITE / "index.html").exists():
        print("site/index.html is missing. Run: make pages", file=sys.stderr)
        return 2
    browser = chrome()
    if not browser:
        print("Chrome is not installed, so this check cannot run.", file=sys.stderr)
        print("It is not part of `make ci` for that reason.", file=sys.stderr)
        return 2

    widths = [int(a) for a in sys.argv[1:]] or WIDTHS
    server, port = serve()
    failed = 0
    try:
        for width in widths:
            print(f"\n{width}px")
            for page in pages():
                result = measure(browser, port, page, width)
                ok = "over=none" in result
                failed += 0 if ok else 1
                mark = "  ok  " if ok else "  OVER"
                print(f"{mark} {page:<20} {result}")
    finally:
        server.shutdown()

    print()
    if failed:
        print(f"{failed} page/width combinations run off the side.")
        return 1
    print(f"No page runs off the side at {', '.join(f'{w}px' for w in widths)}.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

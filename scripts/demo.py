#!/usr/bin/env python3
"""Build a fake ~/.claude to take braids' screenshots against.

The screenshots on the site and in the README are real braids output, not
mockups drawn by hand — a mockup drifts from the program the moment either
changes, and a frame whose borders do not line up is the first thing a reader
notices. This writes a plausible corpus, indexes it, and prints the frames.

Nothing here touches your own ~/.claude. Everything lands under --out.

    python3 scripts/demo.py --out /tmp/braids-demo --frames /tmp/braids-frames
"""

import argparse
import json
import os
import pathlib
import re
import subprocess
import time
import datetime as dt

# One story per conversation, told in turns. A turn is (who, what, tool) where
# tool is None for plain text — enough shape for the spine to have runs to
# collapse, failures to mark and junctions to draw.
STORIES = {
    "checkout-flow": [
        ("you", "checkout stalls at 80% under load and then recovers", None),
        ("claude", "reading the handler and the lock it takes", "Read"),
        ("claude", "grep for the lock across the service", "Grep"),
        ("claude", "reproducing with 200 concurrent carts", "Bash"),
        ("claude", "the lock is held across a network call", None),
        ("you", "which network call", None),
        ("claude", "listing the calls inside the critical section", "Grep"),
        ("claude", "it waits on the tax service, median 40ms", "Bash"),
        ("claude", "tax service timed out, no route to host", "Bash"),
        ("you", "skip the tax call and see if the stall goes", None),
        ("claude", "patching the handler to defer the tax lookup", "Edit"),
        ("claude", "rerunning the load test", "Bash"),
        ("claude", "no stall at 200, none at 500 either", None),
        ("you", "good. what does that break", None),
        ("claude", "the receipt total is computed before tax now", None),
        ("you", "so we need the deferral to rejoin before receipt", None),
    ],
    "try-option-c": [
        ("you", "try option c: move the lock inside the tax client", None),
        ("claude", "moving the lock and narrowing its scope", "Edit"),
        ("claude", "load test at 200 concurrent", "Bash"),
        ("claude", "stall is gone and the receipt total is right", None),
        ("you", "keep this one", None),
    ],
    "cache-gate-probe": [
        ("you", "is the cache gate even open in staging", None),
        ("claude", "checking the gate config", "Read"),
        ("claude", "the gate is closed in staging, open in prod", None),
    ],
    "blobstore-density": [
        ("you", "why is the blobstore doing 11 KiB reads", None),
        ("claude", "sampling the read sizes", "Bash"),
        ("claude", "the reader asks per object, not per batch", None),
        ("you", "batch it", None),
    ],
    "index-contention": [
        ("you", "two writers contend on the same shard index", None),
        ("claude", "counting lock waits per shard", "Bash"),
        ("claude", "one shard takes 80% of the waits", None),
    ],
    "import-pipeline": [
        ("you", "the nightly import drops rows without saying so", None),
        ("claude", "counting rows in and rows out", "Bash"),
        ("claude", "1,412 in, 1,398 out, no error logged", None),
        ("you", "find the fourteen", None),
        ("claude", "diffing the ids", "Bash"),
        ("claude", "all fourteen have a null in a not-null column", None),
    ],
}

# id, story, the title the harness gave it, age in minutes, (parent, forked at
# turn), whether it ends owing you an answer.
#
# Titles are sentences because that is what Claude Code writes. Short slugs
# left the map with a gutter down the middle and made every screenshot look
# emptier than the real thing.
LANES = [
    ("a1b2c3d4-0000-4000-8000-000000000001", "checkout-flow",
     "Checkout stalls at 80% under load, then recovers on its own", 8, None, True),
    ("b2c3d4e5-0000-4000-8000-000000000002", "try-option-c",
     "Option C: move the lock inside the tax client and re-run the load test", 3,
     ("a1b2c3d4-0000-4000-8000-000000000001", 14), False),
    ("c3d4e5f6-0000-4000-8000-000000000003", "cache-gate-probe",
     "Is the cache gate even open in staging, or has it been closed all along", 190,
     ("b2c3d4e5-0000-4000-8000-000000000002", 3), True),
    ("d4e5f6a7-0000-4000-8000-000000000004", "blobstore-density",
     "Blobstore is doing 11 KiB reads per object instead of batching them", 2880,
     ("a1b2c3d4-0000-4000-8000-000000000001", 8), False),
    ("e5f6a7b8-0000-4000-8000-000000000005", "index-contention",
     "Two writers contending on the same shard index, 80% of the waits", 20160,
     ("a1b2c3d4-0000-4000-8000-000000000001", 6), False),
    ("f6a7b8c9-0000-4000-8000-000000000006", "import-pipeline",
     "Nightly import drops rows without logging any of them", 7200, None, True),
]

MEMORIES = {
    "lock-scope-rule": ("project", "A lock must not span a network call. This bit us in checkout",
                        "The checkout handler held its lock across the tax service call, so a 40ms\n"
                        "median turned into a stall at 200 concurrent carts. The rule: **narrow the\n"
                        "lock to the state it protects**, and let the remote call happen outside it.\n"
                        "\n"
                        "## How it showed up\n"
                        "\n"
                        "- 80% of carts completed, the rest sat until the tax call timed out\n"
                        "- `p99` went to 611ms while the median stayed at 41ms\n"
                        "- the receipt total is computed after the deferral, so it has to rejoin\n"
                        "\n"
                        "See [[tax-service-latency]] for what the call actually costs.\n"),
    "tax-service-latency": ("reference", "What the tax service actually costs, measured",
                            "Median 40ms, p99 610ms, and it times out rather than erroring when the\n"
                            "route is down. Anything that waits on it synchronously inherits that p99.\n"),
    "import-nulls": ("project", "The nightly import drops rows with a null in a not-null column",
                     "Fourteen rows of 1,412 on the night we looked. It does not log them. The\n"
                     "insert fails per row and the batch carries on. Related: [[lock-scope-rule]].\n"),
    "working-style": ("feedback", "Read the code before claiming what it does",
                      "Corrected twice for asserting behaviour from a symbol's existence. Verify\n"
                      "against the code path, and say what you ran.\n"),
}

# Work products: how big, and what a line of it looks like. The size is what
# the browser sorts on and the line is what the peek screen shows, so a file
# padded with "{}" makes an honest screenshot of a screen with nothing in it.
# {i} counts up, so a long file does not repeat one line forever.
ARTIFACTS = {
    "tmp/load-test-500.json": (
        611_000,
        '{{"t":{i},"concurrency":500,"p50_ms":38,"p99_ms":611,"stalled":false}}'),
    "tmp/load-test-200.json": (
        242_000,
        '{{"t":{i},"concurrency":200,"p50_ms":41,"p99_ms":98,"stalled":false}}'),
    "tmp/row-ids-in.txt": (74_000, "row-{i:06d}"),
    "tmp/row-ids-out.txt": (73_000, "row-{i:06d}"),
    "tmp/lock-waits-by-shard.tsv": (18_400, "shard-{i:03d}\t{i}\t611"),
    "tmp/handler.orig.go": (
        9_100,
        "\tif err := tax.Quote(ctx, cart); err != nil {{ // line {i}"),
    "tmp/node_modules/left-pad/index.js": (900, "module.exports = pad; // {i}"),
}


def fill(size: int, line: str) -> bytes:
    """Repeat line, counting up, until it is about `size` bytes."""
    out, i = [], 1
    total = 0
    while total < size:
        text = line.format(i=i) + "\n"
        out.append(text)
        total += len(text)
        i += 1
    return "".join(out).encode()


def write_corpus(out: pathlib.Path) -> None:
    project = out / "projects" / "-Users-you-src-storefront"
    project.mkdir(parents=True, exist_ok=True)
    now = dt.datetime(2026, 9, 4, 12, 0, tzinfo=dt.timezone.utc)

    for sid, story_key, title, age, parent, owes in LANES:
        story = STORIES[story_key]
        start = now - dt.timedelta(minutes=age + len(story))
        lines = [json.dumps({"type": "ai-title", "aiTitle": title, "sessionId": sid})]
        prev = None
        for i, (who, text, tool) in enumerate(story):
            at = (start + dt.timedelta(minutes=i)).isoformat().replace("+00:00", "Z")
            uid = f"{sid[:8]}-{i}"
            if who == "you":
                lines.append(json.dumps({
                    "type": "user", "uuid": uid, "parentUuid": prev, "timestamp": at,
                    "cwd": "/Users/you/src/storefront",
                    "message": {"role": "user", "content": text},
                }))
            elif tool:
                lines.append(json.dumps({
                    "type": "assistant", "uuid": uid, "parentUuid": prev, "timestamp": at,
                    "message": {"role": "assistant", "content": [
                        {"type": "tool_use", "id": f"t{i}", "name": tool, "input": {"command": text}},
                    ]},
                }))
                # The one failure, so the spine has an error mark to draw.
                failed = "timed out" in text
                lines.append(json.dumps({
                    "type": "user", "uuid": uid + "r", "parentUuid": uid, "timestamp": at,
                    "message": {"role": "user", "content": [
                        {"type": "tool_result", "tool_use_id": f"t{i}",
                         "is_error": failed, "content": "no route to host" if failed else "ok"},
                    ]},
                }))
                prev = uid + "r"
                continue
            else:
                lines.append(json.dumps({
                    "type": "assistant", "uuid": uid, "parentUuid": prev, "timestamp": at,
                    "message": {"role": "assistant", "content": [{"type": "text", "text": text}]},
                }))
            prev = uid
        if not owes:
            lines = lines[:-1]
        path = project / f"{sid}.jsonl"
        path.write_text("\n".join(lines) + "\n")
        stamp = time.time() - age * 60
        os.utime(path, (stamp, stamp))

    memory = project / "memory"
    memory.mkdir(exist_ok=True)
    index = ["# Memory index", ""]
    for name, (kind, description, body) in MEMORIES.items():
        (memory / f"{name}.md").write_text(
            f"---\nname: {name}\ndescription: {description}\nmetadata:\n"
            f"  type: {kind}\n  originSessionId: {LANES[0][0]}\n---\n\n{body}")
        # working-style is deliberately left out of the index, so the memory
        # screen has the failure it exists to show.
        if name != "working-style":
            index.append(f"- [{name.replace('-', ' ').capitalize()}]({name}.md): {description}")
    (memory / "MEMORY.md").write_text("\n".join(index) + "\n")

    for rel, (size, line) in ARTIFACTS.items():
        path = out / "jobs" / LANES[0][0][:8] / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(fill(size, line))
    (out / "jobs" / LANES[0][0][:8] / "state.json").write_text('{"state":"working"}\n')

    origins = {sid: {"Parent": p[0], "ForkSeq": p[1]} for sid, _, _, _, p, _ in LANES if p}
    braids = out / "braids"
    braids.mkdir(exist_ok=True)
    (braids / "origins.json").write_text(json.dumps(origins, indent=1))


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True, help="where to build the fake ~/.claude")
    ap.add_argument("--frames", help="write the screenshots here as .ans and .txt")
    ap.add_argument("--width", type=int, default=138,
                    help="the width at which no screen elides anything")
    ap.add_argument("--braids", default="braids", help="the binary to run")
    ap.add_argument("--shots", default="go run ./scripts/shots",
                    help="the screenshot tool, for the screens --print cannot reach")
    args = ap.parse_args()

    out = pathlib.Path(args.out).expanduser()
    if out.exists():
        subprocess.run(["rm", "-rf", str(out)], check=True)
    write_corpus(out)

    db = out / "braids" / "index.db"
    subprocess.run([args.braids, "index", "--root", str(out / "projects"), "--db", str(db)],
                   check=True, capture_output=True)

    # The mark, taken from `braids help` so the page cannot drift from
    # internal/brand. Seven lines: the art and the tagline under it.
    if args.frames:
        helped = subprocess.run([args.braids, "help"], check=True,
                                capture_output=True, text=True).stdout
        mark = "\n".join(helped.split("\n")[:7]).rstrip()
        into = pathlib.Path(args.frames).expanduser()
        into.mkdir(parents=True, exist_ok=True)
        (into / "mark.ans").write_text(mark + "\n")
        (into / "mark.txt").write_text(plain(mark) + "\n")

    lane = LANES[0][0][:8]
    # name -> (height, arguments). Every frame comes from scripts/shots, which
    # can reach the screens you get to with keys and can leave the ASCII mark
    # out. That matters more than it sounds: the map needs 108 columns for its
    # facts, its glyph key and all fourteen bindings, and 195 for those plus
    # the mark. Captured at 195 the frames were mostly decoration and the text
    # came out tiny wherever they were shown. The mark lives on the page now.
    #
    # The bin comes last: filling it moves a file out of the work products,
    # and the work frames are taken before that happens.
    shots = {
        "map": (18, []),
        "spine": (24, ["--screen", "spine", "--lane", lane]),
        "search": (18, ["--query", "lock"]),
        "work": (11, ["--screen", "work", "--lane", lane]),
        "file": (18, ["--screen", "file", "--lane", lane]),
        "memories": (15, ["--screen", "memories"]),
        "memory": (20, ["--screen", "memory"]),
        "bin": (9, ["--screen", "bin", "--discard",
                    str(out / "jobs" / lane / "tmp" / "row-ids-out.txt")]),
    }
    for name, (height, extra) in shots.items():
        frame = subprocess.run(
            args.shots.split() + ["--db", str(db), "--root", str(out / "projects"),
                                  "--no-mark", "--width", str(args.width),
                                  "--height", str(height)] + extra,
            check=True, capture_output=True, text=True).stdout
        frame = sanitize(frame, str(db))
        if args.frames:
            into = pathlib.Path(args.frames).expanduser()
            into.mkdir(parents=True, exist_ok=True)
            # .ans keeps braids' own colours. .txt is the same frame with the
            # escapes taken out, for a plain-text paste.
            (into / f"{name}.ans").write_text(frame + "\n")
            (into / f"{name}.txt").write_text(plain(frame) + "\n")
        print(f"=== {name} ===\n{frame}\n")


SGR = re.compile(r"\x1b\[[0-9;]*m")


def plain(frame: str) -> str:
    return SGR.sub("", frame)


def sanitize(frame: str, db: str) -> str:
    """Make a captured frame safe to publish, without disturbing its layout.

    Two things have to go: the index path, which is wherever the demo built it,
    and the rows of raw session IDs that the map prints for a lane it has no
    title for. Both are done on the visible text while the escape sequences are
    left where they are, and the index path is padded back to the width it had,
    because the header lays the facts and the key hints out on one line. An
    earlier version replaced that whole line and quietly deleted the
    `open spine` hint from every screenshot on the site.
    """
    shown = "~/.braids/index.db"
    if db in frame:
        frame = frame.replace(db, shown + " " * max(0, len(db) - len(shown)))
    kept = []
    for line in frame.split("\n"):
        bare = plain(line)
        # The footer prints the selected lane's whole session ID. A made-up one
        # is the single line of a screenshot that looks made up, so it goes.
        if re.match(r"^ [0-9a-f]{8}-", bare):
            continue
        kept.append(line.rstrip())
    # Interior blank lines are part of the layout and stay. Only the trailing
    # ones, left by the footer going, come off.
    while kept and not plain(kept[-1]).strip():
        kept.pop()
    return "\n".join(kept)


if __name__ == "__main__":
    main()

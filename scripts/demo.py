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
        ("claude", "tax service timed out — no route to host", "Bash"),
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

# id, title, age in minutes, (parent, forked at turn), whether it ends owing you
LANES = [
    ("a1b2c3d4-0000-4000-8000-000000000001", "checkout-flow", 8, None, True),
    ("b2c3d4e5-0000-4000-8000-000000000002", "try-option-c", 3, ("a1b2c3d4-0000-4000-8000-000000000001", 14), False),
    ("c3d4e5f6-0000-4000-8000-000000000003", "cache-gate-probe", 190, ("b2c3d4e5-0000-4000-8000-000000000002", 3), True),
    ("d4e5f6a7-0000-4000-8000-000000000004", "blobstore-density", 2880, ("a1b2c3d4-0000-4000-8000-000000000001", 8), False),
    ("e5f6a7b8-0000-4000-8000-000000000005", "index-contention", 20160, ("a1b2c3d4-0000-4000-8000-000000000001", 6), False),
    ("f6a7b8c9-0000-4000-8000-000000000006", "import-pipeline", 7200, None, True),
]

MEMORIES = {
    "lock-scope-rule": ("project", "A lock must not span a network call — this bit us in checkout",
                        "The checkout handler held its lock across the tax service call, so a 40ms\n"
                        "median turned into a stall at 200 concurrent carts. The rule: **narrow the\n"
                        "lock to the state it protects**, and let the remote call happen outside it.\n"
                        "See [[tax-service-latency]].\n"),
    "tax-service-latency": ("reference", "What the tax service actually costs, measured",
                            "Median 40ms, p99 610ms, and it times out rather than erroring when the\n"
                            "route is down. Anything that waits on it synchronously inherits that p99.\n"),
    "import-nulls": ("project", "The nightly import drops rows with a null in a not-null column",
                     "Fourteen rows of 1,412 on the night we looked. It does not log them — the\n"
                     "insert fails per row and the batch carries on. Related: [[lock-scope-rule]].\n"),
    "working-style": ("feedback", "Read the code before claiming what it does",
                      "Corrected twice for asserting behaviour from a symbol's existence. Verify\n"
                      "against the code path, and say what you ran.\n"),
}

ARTIFACTS = {
    "tmp/load-test-200.json": 242_000,
    "tmp/load-test-500.json": 611_000,
    "tmp/lock-waits-by-shard.tsv": 18_400,
    "tmp/handler.orig.go": 9_100,
    "tmp/row-ids-in.txt": 74_000,
    "tmp/row-ids-out.txt": 73_000,
    "tmp/node_modules/left-pad/index.js": 900,
}


def write_corpus(out: pathlib.Path) -> None:
    project = out / "projects" / "-Users-you-src-storefront"
    project.mkdir(parents=True, exist_ok=True)
    now = dt.datetime(2026, 9, 4, 12, 0, tzinfo=dt.timezone.utc)

    for sid, title, age, parent, owes in LANES:
        story = STORIES[title]
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
            index.append(f"- [{name.replace('-', ' ').capitalize()}]({name}.md) — {description}")
    (memory / "MEMORY.md").write_text("\n".join(index) + "\n")

    for rel, size in ARTIFACTS.items():
        path = out / "jobs" / LANES[0][0][:8] / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(b"{}\n" * (size // 3))
    (out / "jobs" / LANES[0][0][:8] / "state.json").write_text('{"state":"working"}\n')

    origins = {sid: {"Parent": p[0], "ForkSeq": p[1]} for sid, _, _, p, _ in LANES if p}
    braids = out / "braids"
    braids.mkdir(exist_ok=True)
    (braids / "origins.json").write_text(json.dumps(origins, indent=1))


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True, help="where to build the fake ~/.claude")
    ap.add_argument("--frames", help="write the screenshots here as .txt")
    ap.add_argument("--width", type=int, default=78)
    ap.add_argument("--braids", default="braids", help="the binary to run")
    args = ap.parse_args()

    out = pathlib.Path(args.out).expanduser()
    if out.exists():
        subprocess.run(["rm", "-rf", str(out)], check=True)
    write_corpus(out)

    db = out / "braids" / "index.db"
    subprocess.run([args.braids, "index", "--root", str(out / "projects"), "--db", str(db)],
                   check=True, capture_output=True)

    shots = {
        "map": [],
        "spine": ["--lane", LANES[0][0][:8]],
        "search": ["--query", "lock"],
    }
    for name, extra in shots.items():
        frame = subprocess.run(
            [args.braids, "map", "--print", "--width", str(args.width), "--db", str(db)] + extra,
            check=True, capture_output=True, text=True).stdout
        frame = strip(frame)
        if args.frames:
            target = pathlib.Path(args.frames).expanduser()
            target.mkdir(parents=True, exist_ok=True)
            (target / f"{name}.txt").write_text(frame + "\n")
        print(f"=== {name} ===\n{frame}\n")


def strip(frame: str) -> str:
    import re
    frame = re.sub(r"\x1b\[[0-9;]*m", "", frame)
    frame = re.sub(r"^ Index:.*$", " Index:          ~/.braids/index.db", frame, flags=re.M)
    kept = [line.rstrip() for line in frame.split("\n")
            if line.strip() and not re.match(r"^ [0-9a-f]{8}-", line)]
    return "\n".join(kept)


if __name__ == "__main__":
    main()

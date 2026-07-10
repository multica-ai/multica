#!/usr/bin/env python3
"""Make a graphify graph.json reproducible across machines.

graphify records imports that resolve OUTSIDE the build scope (e.g. a
cerebro-* package importing @multica/core) using the file's ABSOLUTE path,
slugified into the node id (e.g. "..._firtal_cerebro_packages_core_index_ts").
That absolute prefix differs on every checkout — CI runner, the staging Mac
mini, each agent's worktree, even /tmp vs /private/tmp — so without this step
the committed map would churn on every rebuild and never match across machines.

Fix: strip the machine-specific prefix by keying on the repo directory name
(always the last path segment of the build root), which is identical
everywhere. Everything up to and including "<repo>_" is the volatile absolute
prefix; what follows is the repo-relative slug we keep. Then rewrite with
sorted keys + a stable separator so byte diffs are meaningful.

Usage: graphify-normalize.py <graph.json> [--root <abs-repo-root>]
"""
import json, re, sys, os

def slug(p: str) -> str:
    return re.sub(r"[^a-z0-9]+", "_", p.lower()).strip("_")

def main() -> int:
    args = sys.argv[1:]
    root = None
    if "--root" in args:
        i = args.index("--root")
        root = args[i + 1]
        del args[i:i + 2]
    if not args:
        print("usage: graphify-normalize.py <graph.json> [--root <abs-repo-root>]", file=sys.stderr)
        return 2
    path = args[0]
    root = os.path.realpath(root or os.getcwd())
    repo_name = os.path.basename(root)            # e.g. "firtal-cerebro"

    raw = open(path, "r", encoding="utf-8").read()

    # 1) Raw absolute paths: strip any ".../<repo>/" prefix down to repo-relative.
    raw = re.sub(r"/[^\"]*?/" + re.escape(repo_name) + r"/", "", raw)
    raw = raw.replace(root + "/", "").replace(root, "")

    # 2) Slugified node ids: strip any "<machine-prefix>_<repo_slug>_" down to the
    #    repo-relative slug. The class stops at the quote, so in-scope ids that
    #    never contain the repo slug are left untouched.
    repo_slug = slug(repo_name)                   # e.g. "firtal_cerebro"
    raw = re.sub(r"[a-z0-9_]*" + re.escape(repo_slug) + r"_", "", raw)

    data = json.loads(raw)
    # Drop volatile counters that carry no structural meaning.
    for k in ("input_tokens", "output_tokens"):
        data.pop(k, None)

    with open(path, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
        f.write("\n")
    return 0

if __name__ == "__main__":
    raise SystemExit(main())

#!/usr/bin/env bash
# Build the committed cerebro code-map (graphify-out/graph.json).
#
# This is the ONE command that produces the reproducible, machine-independent
# map that ships in the repo. CI runs it on every merge to main; a developer or
# agent runs it after changing cerebro code so the committed map stays fresh.
#
#   scripts/cerebro/build-graphify-map.sh
#
# What it does:
#   1. graphify update . --no-cluster   (offline, no LLM key — Tree-sitter AST only)
#      scoped by .graphifyignore to the cerebro fork surface.
#   2. normalize graph.json so it is byte-identical across checkouts.
#
# Only graphify-out/graph.json is tracked; cache/ and manifest.json stay local
# (see .gitignore).
set -euo pipefail

GRAPHIFY_VERSION="0.8.40"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

if ! command -v graphify >/dev/null 2>&1; then
  echo "graphify not found. Install with: uv tool install graphifyy==${GRAPHIFY_VERSION}" >&2
  echo "(or: pipx install graphifyy==${GRAPHIFY_VERSION})" >&2
  exit 1
fi

have="$(graphify --version 2>/dev/null | awk '{print $NF}')"
if [ "$have" != "$GRAPHIFY_VERSION" ]; then
  echo "warning: graphify $have installed, map is pinned to $GRAPHIFY_VERSION." >&2
  echo "         Rebuild with the pinned version to avoid map drift." >&2
fi

echo "Building cerebro code-map (offline, code-only)…"
graphify update . --no-cluster

echo "Normalizing for cross-machine reproducibility…"
python3 scripts/cerebro/graphify-normalize.py graphify-out/graph.json --root "$repo_root"

echo "Done. Tracked map: graphify-out/graph.json"

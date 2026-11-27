#!/usr/bin/env bash
# Fork-only: computes the next progbu release tag for the programmierbude fork.
#
# Scheme: <upstream version>-progbu.<n>
#   - <upstream version> is the newest stable upstream tag (vX.Y.Z) contained
#     in the commit being released, so the tag never claims code it lacks.
#   - <n> counts up per upstream version and restarts at 1 for a new one.
#
# Usage:
#   scripts/progbu-release-tag.sh [ref]          # print the next tag
#   scripts/progbu-release-tag.sh --push [ref]   # create and push only that tag
#
# Pushing the tag to origin starts the Release workflow, which publishes the
# Docker images to ghcr.io/programmierbude/multica-{backend,web}.
set -euo pipefail

remote="origin"
push=false
if [[ "${1:-}" == "--push" ]]; then
  push=true
  shift
fi
ref="${1:-HEAD}"

base=$(git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' --exclude '*-*' "$ref" 2>/dev/null) || {
  echo "error: no stable upstream tag (vX.Y.Z) reachable from '$ref'; run 'git fetch upstream --tags' first." >&2
  exit 1
}

# Read existing progbu tags from the remote, not local tags, so the counter is
# shared by everyone releasing from the fork.
last=$(git ls-remote --tags --refs "$remote" "refs/tags/${base}-progbu.*" \
  | sed -E "s#.*refs/tags/${base//./\\.}-progbu\.##" \
  | grep -E '^[0-9]+$' | sort -n | tail -1 || true)
next="${base}-progbu.$(( ${last:-0} + 1 ))"

if [[ "$push" != true ]]; then
  echo "$next"
  exit 0
fi

commit=$(git rev-parse --verify "${ref}^{commit}")
git tag -a "$next" "$commit" -m "progbu release $next"
# Push exactly this tag; never 'git push --tags' (it would push upstream tags
# and start a Release run for each of them).
git push "$remote" "refs/tags/$next"
echo "pushed $next -> $commit"

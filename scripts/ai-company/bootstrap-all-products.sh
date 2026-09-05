#!/usr/bin/env bash
# Bootstrap all default product lines under ~/Projects (or PROJECTS_ROOT).
set -euo pipefail

MULTICA_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
ROOT="${PROJECTS_ROOT:-$(dirname "$MULTICA_ROOT")}"
ORG="${GITHUB_ORG:-chenzh}"
CREATE_REPO=0
PUSH=0
SYNC=0
DRY_RUN=0

usage() {
  cat <<EOF
Usage: bootstrap-all-products.sh [options]

Scaffolds (if missing) and optionally GitHub-publishes:
  - music-game-sea  (existing local scaffold OK)
  - landing-tool-a
  - saas-stripe-mvp (optional third line)

Options:
  --root DIR        Projects parent (default: parent of multica)
  --org ORG         GitHub org (default: chenzh)
  --create-repo     gh repo create for each
  --push            git push
  --sync-backlog    sync-backlog-to-issues after bootstrap
  --dry-run
  -h, --help
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --root) ROOT="${2:?}"; shift 2 ;;
    --org) ORG="${2:?}"; shift 2 ;;
    --create-repo) CREATE_REPO=1; shift ;;
    --push) PUSH=1; shift ;;
    --sync-backlog) SYNC=1; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown: $1" >&2; exit 1 ;;
  esac
done

run() {
  [ "$DRY_RUN" -eq 1 ] && { echo "[dry-run] $*"; return 0; }
  "$@"
}

bootstrap_one() {
  local name="$1"
  local scaffold="${2:-}"
  local dir="$ROOT/$name"
  local repo="$ORG/$name"

  echo ""
  echo "======== $name ========"

  if [ -n "$scaffold" ] && [ ! -f "$dir/package.json" ] && [ ! -f "$dir/README.md" ]; then
    run bash "$MULTICA_ROOT/scripts/ai-company/$scaffold" "$dir"
  elif [ ! -d "$dir/.delivery" ]; then
    run bash "$MULTICA_ROOT/.ai-company/harness/install.sh" "$dir"
  else
    echo "exists: $dir"
  fi

  local args=(--repo "$repo")
  [ "$CREATE_REPO" -eq 1 ] && args+=(--create-repo)
  [ "$PUSH" -eq 1 ] && args+=(--push)
  [ "$DRY_RUN" -eq 1 ] && args+=(--dry-run)

  run bash "$MULTICA_ROOT/scripts/ai-company/bootstrap-project.sh" "$dir" "${args[@]}"

  if [ "$SYNC" -eq 1 ] && [ -f "$dir/.delivery/$name/backlog.md" ]; then
    local sync_args=(--backlog "$dir/.delivery/$name/backlog.md" --repo "$repo")
    [ "$DRY_RUN" -eq 1 ] && sync_args+=(--dry-run)
    run bash "$MULTICA_ROOT/scripts/ai-company/sync-backlog-to-issues.sh" "${sync_args[@]}"
  fi
}

# music-game-sea: only harness if dir exists without full scaffold
if [ -d "$ROOT/music-game-sea" ]; then
  bootstrap_one music-game-sea ""
else
  echo "music-game-sea: create manually or copy from multica examples first"
fi

bootstrap_one landing-tool-a scaffold-landing.sh
bootstrap_one saas-stripe-mvp scaffold-saas.sh

echo ""
echo "Done. CEO dashboard:"
echo "  GITHUB_ORG=$ORG bash $MULTICA_ROOT/scripts/ai-company/ceo-dashboard.sh"

#!/usr/bin/env bash
# Sync agent-safe tickets from .ai-company/examples/<delivery_slug>/backlog.md for active registry projects.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=lib/source-local-env.sh
source "$SCRIPT_DIR/lib/source-local-env.sh"

REGISTRY="${REGISTRY:-$MULTICA_ROOT/.ai-company/templates/project-registry.yaml}"
GITHUB_ORG="${GITHUB_ORG:-chenzh}"
DRY_RUN=0
SKIP_EXISTING=1

usage() {
  cat <<'EOF'
Usage: sync-portfolio-backlogs.sh [options]

For each non-paused project in project-registry.yaml, if
.ai-company/examples/<delivery_slug>/backlog.md exists, create missing issues.

Options:
  --registry PATH
  --org ORG
  --dry-run
  --no-skip-existing   Create even when [TICKET-xxx] issue already exists
  -h, --help
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --registry) REGISTRY="${2:?}"; shift 2 ;;
    --org) GITHUB_ORG="${2:?}"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    --no-skip-existing) SKIP_EXISTING=0; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

pairs="$(
  python3 - "$REGISTRY" "$GITHUB_ORG" "$MULTICA_ROOT" <<'PY'
import sys
from pathlib import Path

registry = Path(sys.argv[1])
org = sys.argv[2]
root = Path(sys.argv[3])
projects = []
current: dict[str, str] = {}

def flush():
    global current
    if not current.get("id"):
        return
    if current.get("paused") == "true":
        current = {}
        return
    slug = current.get("delivery_slug", "")
    repo = current.get("repo", "")
    if slug and repo:
        backlog = root / ".ai-company/examples" / slug / "backlog.md"
        if backlog.is_file():
            print(f"{slug}\t{repo}\t{backlog}")
    current = {}

for line in registry.read_text(encoding="utf-8").splitlines():
    s = line.strip()
    if s.startswith("- id:"):
        flush()
        current = {"id": s.split(":", 1)[1].strip()}
        continue
    if not current:
        continue
    if s.startswith("paused:"):
        current["paused"] = s.split(":", 1)[1].strip()
    elif s.startswith("delivery_slug:"):
        current["delivery_slug"] = s.split(":", 1)[1].strip()
    elif s.startswith("repo:"):
        repo = s.split(":", 1)[1].strip()
        repo = repo.replace("github.com/", "").replace("https://github.com/", "")
        if repo.startswith("your-org/"):
            repo = repo.replace("your-org/", f"{org}/", 1)
        current["repo"] = repo
flush()
PY
)"

if [ -z "$pairs" ]; then
  echo "sync-portfolio-backlogs: no active backlogs found"
  exit 0
fi

while IFS=$'\t' read -r slug repo backlog; do
  [ -z "$slug" ] && continue
  echo "→ $slug ($repo)"
  args=(--backlog "$backlog" --repo "$repo")
  if [ "$DRY_RUN" -eq 1 ]; then
    args+=(--dry-run)
  fi
  if [ "$SKIP_EXISTING" -eq 1 ]; then
    args+=(--skip-existing)
  fi
  bash "$SCRIPT_DIR/sync-backlog-to-issues.sh" "${args[@]}"
done <<<"$pairs"

echo "sync-portfolio-backlogs: done"

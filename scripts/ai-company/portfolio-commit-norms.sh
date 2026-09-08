#!/usr/bin/env bash
# Stage and optionally commit company-os norms + CLAUDE.md across portfolio checkouts.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=lib/source-local-env.sh
source "$SCRIPT_DIR/lib/source-local-env.sh"

REGISTRY="${REGISTRY:-$MULTICA_ROOT/.ai-company/templates/project-registry.yaml}"
DRY_RUN=0
DO_COMMIT=0
DO_PUSH=0
MESSAGE="chore: sync company-os norms and CLAUDE.md from multica HQ"

usage() {
  cat <<'EOF'
Usage: portfolio-commit-norms.sh [options]

For each project in project-registry.yaml with a local checkout:
  git add CLAUDE.md .delivery/company-os .delivery/COMPANY-OS.md
  optional: commit and/or push

Options:
  --commit            Create git commit when there are staged changes
  --push              git push origin HEAD after commit
  --message MSG       Commit message
  --dry-run           Print actions only
  -h, --help

Does not add .delivery/.agent-runs/ (local dispatch logs).
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --commit) DO_COMMIT=1; shift ;;
    --push) DO_PUSH=1; DO_COMMIT=1; shift ;;
    --message) MESSAGE="${2:?}"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

ids="$(
  python3 - "$REGISTRY" <<'PY'
import sys
from pathlib import Path

registry = Path(sys.argv[1])
current: dict[str, str] = {}

def flush():
    global current
    if current.get("id") and current.get("paused") != "true":
        print(current["id"])
    current = {}

for line in registry.read_text(encoding="utf-8").splitlines():
    s = line.strip()
    if s.startswith("- id:"):
        flush()
        current = {"id": s.split(":", 1)[1].strip()}
    elif current and s.startswith("paused:"):
        current["paused"] = s.split(":", 1)[1].strip()
flush()
PY
)"

ok=0
skip=0

run() {
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] $*"
  else
    "$@"
  fi
}

while IFS= read -r pid; do
  [ -n "$pid" ] || continue
  echo ">> $pid"
  if ! path="$(bash "$SCRIPT_DIR/resolve-repo-path.sh" --id "$pid" --quiet 2>/dev/null)"; then
    echo "   skip: no local path" >&2
    skip=$((skip + 1))
    continue
  fi
  if [ ! -d "$path/.git" ]; then
    echo "   skip: not a git repo" >&2
    skip=$((skip + 1))
    continue
  fi

  files=()
  [ -f "$path/CLAUDE.md" ] && files+=("CLAUDE.md")
  [ -d "$path/.delivery/company-os" ] && files+=(".delivery/company-os")
  [ -f "$path/.delivery/COMPANY-OS.md" ] && files+=(".delivery/COMPANY-OS.md")

  if [ "${#files[@]}" -eq 0 ]; then
    echo "   skip: nothing to add (run sync-company-norms.sh first)" >&2
    skip=$((skip + 1))
    continue
  fi

  run git -C "$path" add "${files[@]}"

  if [ "$DO_COMMIT" -ne 1 ]; then
    echo "   staged: ${files[*]} (use --commit)"
    ok=$((ok + 1))
    continue
  fi

  if [ "$DRY_RUN" -eq 1 ]; then
    echo "   [dry-run] git commit -m \"$MESSAGE\""
    ok=$((ok + 1))
    continue
  fi

  if git -C "$path" diff --cached --quiet; then
    if [ "$DO_PUSH" -eq 1 ] && [ "$DRY_RUN" -eq 0 ]; then
      branch="$(git -C "$path" branch --show-current)"
      if git -C "$path" rev-parse "@{u}" &>/dev/null; then
        ahead="$(git -C "$path" rev-list --count "@{u}..HEAD" 2>/dev/null || echo 0)"
        if [ "${ahead:-0}" -gt 0 ]; then
          run git -C "$path" push origin "$branch"
          echo "   pushed origin/$branch ($ahead commits)"
          ok=$((ok + 1))
          continue
        fi
      else
        run git -C "$path" push -u origin "$branch"
        echo "   pushed origin/$branch (set upstream)"
        ok=$((ok + 1))
        continue
      fi
    fi
    echo "   nothing to commit"
    skip=$((skip + 1))
    continue
  fi

  run git -C "$path" commit -m "$MESSAGE"
  echo "   committed on $(git -C "$path" branch --show-current)"

  if [ "$DO_PUSH" -eq 1 ]; then
    branch="$(git -C "$path" branch --show-current)"
    run git -C "$path" push origin "$branch"
    echo "   pushed origin/$branch"
  fi
  ok=$((ok + 1))
done <<<"$ids"

echo ""
echo "portfolio-commit-norms: ok=$ok skip=$skip commit=$DO_COMMIT push=$DO_PUSH"

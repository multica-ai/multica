#!/usr/bin/env bash
# Install content-harness into a target git repository (Hermes media machine).
set -euo pipefail

DRY_RUN=0
FORCE=0
TARGET=""

usage() {
  cat <<'EOF'
Usage: install-content-harness.sh [options] [TARGET_DIR]

Installs content delivery harness for remote Hermes workers:
  .delivery/ prompts + template
  scripts/content-delivery/
  .github/workflows/content-delivery-dispatch.yml
  .github/ISSUE_TEMPLATE/content_agent_safe_task.yml

Options:
  --dry-run    Print actions without copying
  --force      Overwrite existing files
  -h, --help   Show help

Example:
  bash .ai-company/harness/install-content-harness.sh ../content-youtube-sea
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) DRY_RUN=1; shift ;;
    --force) FORCE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    -*) echo "Unknown option: $1" >&2; usage; exit 1 ;;
    *)
      if [ -n "$TARGET" ]; then
        echo "Unexpected argument: $1" >&2
        exit 1
      fi
      TARGET="$1"
      shift
      ;;
  esac
done

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SOURCE_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SCAFFOLD="$SCRIPT_DIR/scaffold-content"

if [ -n "${TARGET:-}" ]; then
  mkdir -p "$TARGET"
  TARGET="$(cd "$TARGET" && pwd)"
else
  TARGET="$(pwd)"
fi

run() {
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] $*"
  else
    "$@"
  fi
}

copy_tree() {
  local src="$1"
  local dst="$2"
  if [ ! -e "$src" ]; then
    echo "warning: missing source $src" >&2
    return 0
  fi
  if [ -e "$dst" ] && [ "$FORCE" -ne 1 ]; then
    echo "skip (exists): $dst  (use --force to overwrite)"
    return 0
  fi
  run mkdir -p "$(dirname "$dst")"
  if [ -d "$src" ]; then
    run rm -rf "$dst"
    run cp -R "$src" "$dst"
  else
    run cp "$src" "$dst"
  fi
  echo "installed: $dst"
}

echo "content-harness install"
echo "  source: $SOURCE_ROOT"
echo "  target: $TARGET"
echo ""

copy_tree "$SCAFFOLD/.delivery/_template" "$TARGET/.delivery/_template"
run mkdir -p "$TARGET/.delivery/prompts"
copy_tree "$SCRIPT_DIR/content-hq-split.md" "$TARGET/.delivery/CONTENT-HQ-SPLIT.md"
copy_tree "$SCRIPT_DIR/../templates/orchestrator-kickoff-content.md" \
  "$TARGET/.delivery/prompts/orchestrator-kickoff.md"

copy_tree "$SOURCE_ROOT/scripts/content-delivery" "$TARGET/scripts/content-delivery"
copy_tree "$SCAFFOLD/.github/workflows/content-delivery-dispatch.yml" \
  "$TARGET/.github/workflows/content-delivery-dispatch.yml"
copy_tree "$SCAFFOLD/.github/ISSUE_TEMPLATE/content_agent_safe_task.yml" \
  "$TARGET/.github/ISSUE_TEMPLATE/content_agent_safe_task.yml"

run mkdir -p "$TARGET/drafts" "$TARGET/calendar" "$TARGET/brand"
if [ ! -f "$TARGET/brand/voice.md" ] || [ "$FORCE" -eq 1 ]; then
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] write $TARGET/brand/voice.md"
  else
    cat >"$TARGET/brand/voice.md" <<'EOF'
# Brand voice

- Tone:
- Avoid:
- CTA style:
- Disclosure (AI-assisted content):
EOF
    echo "installed: $TARGET/brand/voice.md"
  fi
fi

if [ "$DRY_RUN" -eq 0 ]; then
  chmod +x "$TARGET/scripts/content-delivery/"*.sh 2>/dev/null || true
fi

cat <<'EOF'

Responsibility split (installed to .delivery/CONTENT-HQ-SPLIT.md):
  CEO HQ = queue, nightly, Feishu, product cursor, publish
  Remote  = this repo only: pull-dispatch, Hermes oneshot, drafts/PR

Next steps (remote Hermes machine):
  1. gh auth login (or GH_TOKEN for automation)
  2. hermes setup --portal (or your provider); prefer profile zimeiti
  3. Labels: agent-safe, agent-running, agent-blocked, agent-done
  4. cron 22:00: pull-dispatch.sh --max-tasks 1  (or GHA runner: self-hosted, content-hermes)
  5. Disable Kanban auto-dispatch if still active (Issues = single queue)
  6. Test: bash scripts/content-delivery/pull-dispatch.sh --max-tasks 1 --dry-run

CEO HQ (no local clone required):
  project-registry.yaml: kind: content, dispatch_mode: remote-pull (or gha)
  bash scripts/ai-company/portfolio-dispatch.sh --dry-run --max-total 1

See .delivery/CONTENT-HQ-SPLIT.md and .ai-company/docs/24-content-operations.md
EOF

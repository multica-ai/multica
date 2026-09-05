#!/usr/bin/env bash
# Activate an existing product repo for daytime Autopilot + portfolio dispatch.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=lib/source-local-env.sh
source "$SCRIPT_DIR/lib/source-local-env.sh"
# shellcheck source=lib/site-factory-activate.sh
source "$SCRIPT_DIR/lib/site-factory-activate.sh"

REGISTRY="${REGISTRY:-$MULTICA_ROOT/.ai-company/templates/project-registry.yaml}"
GITHUB_ORG="${GITHUB_ORG:-chenzh}"
PROJECT_ID=""
TARGET=""
REPO=""
STACK="cloudflare-pages"
TIER="experiment"
PRIORITY=12
MAX_NIGHTLY=2
SYNC_BACKLOG=1
TICKET_FROM="TICKET-001"
TICKET_TO="TICKET-005"
DISPATCH=1
MAX_DISPATCH=1
DRY_RUN=0
NOTES=""

usage() {
  cat <<'EOF'
Usage: activate-project-autopilot.sh --id <project-id> --target <dir> [options]

Registers an existing repo for portfolio / employee Autopilot:
  1. Append project-registry.yaml (if missing)
  2. Write repo-paths.local.yaml (if not resolvable)
  3. bootstrap-project.sh (harness + labels + backlog issues)
  4. Optional: dispatch first agent-safe ticket

Options:
  --repo OWNER/NAME     GitHub repo (default: GITHUB_ORG/<basename target>)
  --stack NAME          registry stack field (default: cloudflare-pages)
  --tier TIER           experiment|staging|production
  --priority N          portfolio priority (default: 12)
  --from / --to TICKET  Backlog sync range
  --no-backlog          Skip sync-backlog
  --no-dispatch         Skip initial dispatch
  --max-dispatch N      Initial dispatch count (default: 1)
  --dry-run
  -h, --help

Example (MeiGen AI):
  bash scripts/ai-company/activate-project-autopilot.sh \
    --id meigen-replica \
    --target ~/Projects/meigen-replica \
    --repo chenzh/meigen-replica \
    --stack visual-replica \
    --from TICKET-001 --to TICKET-003
EOF
}

append_registry_entry() {
  local id="$1"
  local repo="$2"
  if grep -q "id: $id" "$REGISTRY" 2>/dev/null; then
    echo "registry: $id already listed"
    return 0
  fi
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] append registry id=$id repo=$repo"
    return 0
  fi
  local note="${NOTES:-activated $(date -u +%Y-%m-%d)}"
  cat >>"$REGISTRY" <<EOF

  - id: $id
    repo: github.com/$repo
    tier: $TIER
    priority: $PRIORITY
    stack: $STACK
    autopilot_id: null
    max_nightly_tickets: $MAX_NIGHTLY
    openapi: false
    e2e: true
    paused: false
    delivery_slug: $id
    notes: "$note"
EOF
  echo "registry: appended $id"
}

dispatch_first_tickets() {
  local repo="$1"
  local root="$2"
  local limit="$3"
  if ! cursor-agent status &>/dev/null 2>&1; then
    echo "warn: cursor-agent not ready — skip dispatch" >&2
    return 0
  fi
  local issues_json
  issues_json="$(gh issue list -R "$repo" -l agent-safe -s open --json number 2>/dev/null || echo '[]')"
  local nums
  nums="$(python3 - "$issues_json" "$limit" <<'PY'
import json, sys
issues = json.loads(sys.argv[1])
limit = int(sys.argv[2])
issues.sort(key=lambda row: row["number"])
for row in issues[:limit]:
    print(row["number"])
PY
)"
  local default_branch
  default_branch="$(gh repo view "$repo" --json defaultBranchRef -q .defaultBranchRef.name 2>/dev/null || echo main)"
  for num in $nums; do
    [ -z "$num" ] && continue
    echo ">> dispatch issue #$num"
    GITHUB_REPOSITORY="$repo" REPO_ROOT="$root" WORKTREE_BASE="$default_branch" \
      bash "$MULTICA_ROOT/scripts/agent-delivery/dispatch-cursor-agent-cli.sh" "$num"
  done
}

while [ $# -gt 0 ]; do
  case "$1" in
    --id) PROJECT_ID="${2:?}"; shift 2 ;;
    --target) TARGET="${2:?}"; shift 2 ;;
    --repo) REPO="${2:?}"; shift 2 ;;
    --stack) STACK="${2:?}"; shift 2 ;;
    --tier) TIER="${2:?}"; shift 2 ;;
    --priority) PRIORITY="${2:?}"; shift 2 ;;
    --from) TICKET_FROM="${2:?}"; shift 2 ;;
    --to) TICKET_TO="${2:?}"; shift 2 ;;
    --no-backlog) SYNC_BACKLOG=0; shift ;;
    --no-dispatch) DISPATCH=0; shift ;;
    --max-dispatch) MAX_DISPATCH="${2:?}"; shift 2 ;;
    --notes) NOTES="${2:?}"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage; exit 1 ;;
  esac
done

if [ -z "$PROJECT_ID" ] || [ -z "$TARGET" ]; then
  usage >&2
  exit 1
fi

TARGET="$(cd "$TARGET" && pwd)"
if [ -z "$REPO" ]; then
  REPO="$GITHUB_ORG/$(basename "$TARGET")"
fi

echo "== activate-project-autopilot =="
echo "  id:     $PROJECT_ID"
echo "  target: $TARGET"
echo "  repo:   $REPO"
echo ""

append_registry_entry "$PROJECT_ID" "$REPO"
site_factory_ensure_local_repo_path "$PROJECT_ID" "$TARGET" "$MULTICA_ROOT" "$SCRIPT_DIR"

boot_args=("$TARGET" --repo "$REPO")
if [ "$SYNC_BACKLOG" -eq 1 ]; then
  boot_args+=(--sync-backlog --from "$TICKET_FROM" --to "$TICKET_TO")
fi
if [ "$DRY_RUN" -eq 1 ]; then
  boot_args+=(--dry-run)
  echo "[dry-run] bootstrap-project.sh ${boot_args[*]}"
else
  bash "$SCRIPT_DIR/bootstrap-project.sh" "${boot_args[@]}"
fi

if [ "$DISPATCH" -eq 1 ] && [ "$DRY_RUN" -eq 0 ]; then
  dispatch_first_tickets "$REPO" "$TARGET" "$MAX_DISPATCH"
fi

cat <<EOF

Autopilot armed when registry lists $PROJECT_ID (paused: false) and agent-safe issues exist.

Daytime loop:  bash scripts/ai-company/autopilot-dispatch.sh
Manual ticket: bash scripts/agent-delivery/dispatch-cursor-agent-cli.sh <N>
CEO workbench: http://127.0.0.1:9477
EOF

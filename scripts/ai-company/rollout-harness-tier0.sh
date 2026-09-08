#!/usr/bin/env bash
# Roll out token-efficient Tier-0 harness rules across OPC HQ + portfolio + local registry repos.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
REGISTRY="${REGISTRY:-$MULTICA_ROOT/.ai-company/templates/project-registry.yaml}"
VAULT="${VAULT:-$HOME/Documents/SecondBrain}"
SKIP_VAULT_SYNC=0
SKIP_PORTFOLIO=0
INCLUDE_PAUSED=0
DRY_RUN=0
FORCE=0

usage() {
  cat <<'EOF'
Usage: rollout-harness-tier0.sh [options]

Installs thin alwaysApply cursor rules (pointers) — never full harness docs.

Steps:
  1. SecondBrain sync-all-harness (vault-harness.mdc + docs/VAULT-HARNESS.md)
  2. zbrain-session.mdc → all local registry + portfolio checkouts
  3. install-harness (company-harness.mdc) + sync-company-norms for portfolio

Options:
  --skip-vault-sync   Skip sync-all-harness.sh
  --skip-portfolio    Skip portfolio install + company-os sync
  --include-paused    Include paused registry projects
  --force             Pass --force to install-harness.sh
  --dry-run           Preview only
  -h, --help
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --skip-vault-sync) SKIP_VAULT_SYNC=1; shift ;;
    --skip-portfolio) SKIP_PORTFOLIO=1; shift ;;
    --include-paused) INCLUDE_PAUSED=1; shift ;;
    --force) FORCE=1; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 1 ;;
  esac
done

run() {
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] $*"
  else
    "$@"
  fi
}

SESSION_TEMPLATE="$VAULT/10-SYSTEM/HARNESS/projection/templates/cursor-rule-session.mdc"

install_session_rule() {
  local repo="$1"
  [ -d "$repo" ] || return 0
  [ -f "$repo/.secondbrain" ] || return 0
  [ -f "$SESSION_TEMPLATE" ] || {
    echo "warn: missing session template: $SESSION_TEMPLATE" >&2
    return 1
  }
  run mkdir -p "$repo/.cursor/rules"
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] cp $SESSION_TEMPLATE -> $repo/.cursor/rules/zbrain-session.mdc"
  else
    cp "$SESSION_TEMPLATE" "$repo/.cursor/rules/zbrain-session.mdc"
    echo "[OK] zbrain-session -> $repo"
  fi
}

echo "=== rollout-harness-tier0 ==="
echo "multica: $MULTICA_ROOT"
echo "vault:   $VAULT"
echo ""

if [ "$SKIP_VAULT_SYNC" -eq 0 ] && [ -f "$VAULT/10-SYSTEM/scripts/sync-all-harness.sh" ]; then
  echo "1. SecondBrain sync-all-harness"
  run bash "$VAULT/10-SYSTEM/scripts/sync-all-harness.sh" --skip-pull
else
  echo "1. skip vault sync"
fi

echo ""
echo "2. zbrain-session.mdc"
install_session_rule "$MULTICA_ROOT"
if [ -f "$VAULT/10-SYSTEM/HARNESS/registry.json" ]; then
  while IFS=$'\t' read -r _ path; do
    [ -n "$path" ] || continue
    install_session_rule "$path" || true
  done < <(python3 - "$VAULT/10-SYSTEM/HARNESS/registry.json" <<'PY'
import json, sys, os
with open(sys.argv[1], encoding="utf-8") as f:
    for p in json.load(f).get("projects", []):
        path = p.get("path", "")
        if path and os.path.isdir(path):
            print(f"{p.get('slug','')}\t{path}")
PY
)
fi

project_ids="$(
  python3 - "$REGISTRY" "$INCLUDE_PAUSED" <<'PY'
import sys
from pathlib import Path

registry = Path(sys.argv[1])
include_paused = sys.argv[2] == "1"
current: dict[str, str] = {}

def flush():
    global current
    if not current.get("id"):
        return
    if current.get("paused") == "true" and not include_paused:
        current = {}
        return
    print(current["id"])
    current = {}

for line in registry.read_text(encoding="utf-8").splitlines():
    s = line.strip()
    if s.startswith("- id:"):
        flush()
        current = {"id": s.split(":", 1)[1].strip()}
    elif s.startswith("paused:") and current:
        current["paused"] = s.split(":", 1)[1].strip()
flush()
PY
)"

while IFS= read -r pid; do
  [ -n "$pid" ] || continue
  if path="$(bash "$SCRIPT_DIR/resolve-repo-path.sh" --id "$pid" --quiet 2>/dev/null)"; then
    install_session_rule "$path" || true
  fi
done <<<"$project_ids"

if [ "$SKIP_PORTFOLIO" -eq 0 ]; then
  echo ""
  echo "3. portfolio harness + company-os"
  sync_args=()
  [ "$INCLUDE_PAUSED" -eq 1 ] && sync_args+=(--include-paused)
  [ "$DRY_RUN" -eq 1 ] && sync_args+=(--dry-run)
  sync_args+=(--harness)
  [ "$FORCE" -eq 1 ] && sync_args+=(--force-harness)
  run bash "$SCRIPT_DIR/sync-company-norms.sh" "${sync_args[@]}"
else
  echo "3. skip portfolio"
fi

echo ""
echo "4. verify Tier-0 + learnings loop"
if [ "$DRY_RUN" -eq 1 ]; then
  echo "[dry-run] verify-harness-tier0.sh && verify-harness-learnings.sh"
else
  bash "$SCRIPT_DIR/verify-harness-tier0.sh" || true
  bash "$SCRIPT_DIR/verify-harness-learnings.sh" || true
fi

echo ""
echo "Done. User Rules: keep personal prefs only — do not duplicate harness text."

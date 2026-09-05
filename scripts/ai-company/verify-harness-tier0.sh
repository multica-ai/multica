#!/usr/bin/env bash
# Verify Tier-0 harness cursor rules (thin pointers) across HQ + portfolio + SecondBrain registry.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
REGISTRY="${REGISTRY:-$MULTICA_ROOT/.ai-company/templates/project-registry.yaml}"
VAULT="${VAULT:-$HOME/Documents/SecondBrain}"
INCLUDE_PAUSED=0
QUIET=0

usage() {
  cat <<'EOF'
Usage: verify-harness-tier0.sh [options]

Checks alwaysApply Tier-0 cursor rules (pointers only — not full harness docs).

Expected rules:
  multica HQ:     vault-harness, zbrain-session, company-harness, code-index
  product + .delivery: vault-harness, zbrain-session, company-harness
  .secondbrain only:   vault-harness, zbrain-session

Options:
  --registry PATH   project-registry.yaml
  --vault PATH      SecondBrain vault root
  --include-paused  Include paused portfolio projects
  --quiet           Only print failures
  -h, --help
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --registry) REGISTRY="${2:?}"; shift 2 ;;
    --vault) VAULT="${2:?}"; shift 2 ;;
    --include-paused) INCLUDE_PAUSED=1; shift ;;
    --quiet) QUIET=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 1 ;;
  esac
done

ok=0
warn=0
fail=0

log_ok() { [ "$QUIET" -eq 1 ] || echo "  ✅ $1"; ok=$((ok + 1)); }
log_warn() { echo "  ⚠️  $1"; warn=$((warn + 1)); }
log_fail() { echo "  ❌ $1"; fail=$((fail + 1)); }

check_rule() {
  local repo="$1" rule="$2"
  [ -f "$repo/.cursor/rules/$rule" ]
}

check_repo() {
  local label="$1" repo="$2" mode="$3"
  [ -d "$repo" ] || return 0

  echo ">> $label ($repo) [$mode]"
  local missing=()

  case "$mode" in
    hq)
      for r in vault-harness.mdc zbrain-session.mdc company-harness.mdc code-index.mdc; do
        check_rule "$repo" "$r" || missing+=("$r")
      done
      ;;
    product)
      check_rule "$repo" "company-harness.mdc" || missing+=("company-harness.mdc")
      if [ -f "$repo/.secondbrain" ]; then
        for r in vault-harness.mdc zbrain-session.mdc; do
          check_rule "$repo" "$r" || missing+=("$r")
        done
      fi
      ;;
    secondbrain)
      for r in vault-harness.mdc zbrain-session.mdc; do
        check_rule "$repo" "$r" || missing+=("$r")
      done
      ;;
  esac

  if [ "${#missing[@]}" -eq 0 ]; then
    log_ok "Tier-0 rules present"
    if [ -f "$repo/.secondbrain" ]; then
      [ -f "$repo/docs/VAULT-HARNESS.md" ] && log_ok "docs/VAULT-HARNESS.md" || log_warn "missing docs/VAULT-HARNESS.md"
    fi
    if [ "$mode" = "product" ]; then
      [ -d "$repo/.delivery/company-os" ] && log_ok ".delivery/company-os/" || log_warn "missing .delivery/company-os/ (run sync-company-norms.sh)"
    fi
    if [ "$mode" = "hq" ]; then
      [ -d "$repo/.ai-company/docs" ] && log_ok ".ai-company/ (HQ authority)" || log_warn "missing .ai-company/"
    fi
    return 0
  fi

  log_fail "missing: ${missing[*]}"
  return 1
}

echo "Harness Tier-0 verification — $(date '+%Y-%m-%d %H:%M')"
echo ""

check_repo "multica (HQ)" "$MULTICA_ROOT" hq

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
    mode="product"
    [ -d "$path/.delivery" ] || mode="secondbrain"
    check_repo "$pid" "$path" "$mode" || true
  else
    echo ">> $pid (skip: no local checkout)"
    warn=$((warn + 1))
  fi
done <<<"$project_ids"

if [ -f "$VAULT/10-SYSTEM/HARNESS/registry.json" ]; then
  while IFS=$'\t' read -r slug path; do
    [ -n "$slug" ] || continue
  [ "$path" = "$MULTICA_ROOT" ] && continue
    if [ -d "$path" ] && [ -f "$path/.secondbrain" ]; then
      already=0
      while IFS= read -r pid; do
        [ -n "$pid" ] || continue
        if p2="$(bash "$SCRIPT_DIR/resolve-repo-path.sh" --id "$pid" --quiet 2>/dev/null)" && [ "$p2" = "$path" ]; then
          already=1
          break
        fi
      done <<<"$project_ids"
      [ "$already" -eq 1 ] && continue
      mode="secondbrain"
      [ -d "$path/.delivery" ] && mode="product"
      check_repo "registry:$slug" "$path" "$mode" || true
    fi
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

echo ""
echo "────────────────────────────"
printf "结果: %s 通过 · %s 警告/跳过 · %s 失败\n" "$ok" "$warn" "$fail"
if [ "$fail" -eq 0 ]; then
  echo "Tier-0 harness 覆盖 OK"
  exit 0
fi
echo "修复: bash scripts/ai-company/rollout-harness-tier0.sh"
exit 1

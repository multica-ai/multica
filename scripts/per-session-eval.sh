#!/usr/bin/env bash
# per-session-eval.sh — chunk-end eval gate
# Runs L3 evals consistently across autonomous-loop chunks
# Outputs pass/fail table + exit 0 (all pass) or exit 1 (any fail)

set -uo pipefail

CHUNK_ID="${1:-unknown}"
REPORT_DIR="docs/upstream-sync/eval-reports"
REPORT_FILE="${REPORT_DIR}/${CHUNK_ID}-$(date -u +%Y%m%dT%H%M%SZ).md"
mkdir -p "$REPORT_DIR"

# Bash 3.2 compatible: use a tmp file as result store (name|status per line)
RESULTS_FILE="$(mktemp -t per-session-eval-results.XXXXXX)"
trap 'rm -f "$RESULTS_FILE"' EXIT
OVERALL_PASS=true

set_result() {
  local name="$1"
  local status="$2"
  # Remove any prior entry for this name, then append
  if [ -s "$RESULTS_FILE" ]; then
    grep -v "^${name}|" "$RESULTS_FILE" >"${RESULTS_FILE}.tmp" || true
    mv "${RESULTS_FILE}.tmp" "$RESULTS_FILE"
  fi
  echo "${name}|${status}" >>"$RESULTS_FILE"
}

get_result() {
  local name="$1"
  awk -F'|' -v n="$name" '$1==n {print $2; exit}' "$RESULTS_FILE"
}

run_check() {
  local name="$1"
  local cmd="$2"
  echo "=== Running: $name ==="
  if eval "$cmd" >/tmp/check_${name//[^a-zA-Z0-9]/_}.log 2>&1; then
    set_result "$name" "PASS"
    echo "[PASS] $name"
  else
    set_result "$name" "FAIL"
    OVERALL_PASS=false
    echo "[FAIL] $name — see /tmp/check_${name//[^a-zA-Z0-9]/_}.log"
  fi
}

# L1 — typecheck across all packages
run_check "typecheck" "pnpm typecheck"

# L1 — unit tests (Vitest across all packages)
run_check "unit-tests-ts" "pnpm test -- --run"

# L1 — Go tests
run_check "go-tests" "make test"

# L2 — Playwright E2E (only if dev server can be started AND baselines exist)
# Heavy; only run for chunks 7+ (Phase 2+) AND only if the corresponding
# baselines have been captured. Per upstream-sync preflight P5 amendment,
# pixel snapshots are deferred to Phase 1+ if flake risk warrants.
case "$CHUNK_ID" in
  chunk-7|chunk-8|chunk-9|chunk-10|chunk-11|chunk-12|phase-2|phase-3*|phase-4|phase-5)
    if [ -f e2e/playwright.config.ts ] && [ -d e2e/__snapshots__ ]; then
      run_check "e2e-playwright" "pnpm exec playwright test --reporter=line"
    else
      set_result "e2e-playwright" "SKIP (baselines not yet captured — P5 amendment defers)"
    fi
    if [ -f e2e/visual.spec.ts ]; then
      run_check "visual-regression" "pnpm exec playwright test e2e/visual.spec.ts --reporter=line"
    else
      set_result "visual-regression" "SKIP (visual.spec.ts not yet created — P5 amendment defers)"
    fi
    ;;
  *)
    set_result "e2e-playwright" "SKIP"
    set_result "visual-regression" "SKIP"
    ;;
esac

# Disciplin — CEREBRO-PATCH markers
if [ -f scripts/validate-cerebro-patches.sh ]; then
  run_check "cerebro-patches" "bash scripts/validate-cerebro-patches.sh"
else
  set_result "cerebro-patches" "SKIP (script not yet created)"
fi

# Bundle size budget (skip until baseline exists)
if [ -f e2e/budgets.json ]; then
  run_check "bundle-budget" "node scripts/measure-bundle.js"
else
  set_result "bundle-budget" "SKIP (baseline not yet captured)"
fi

# Build report
{
  echo "# Eval Report: $CHUNK_ID"
  echo ""
  echo "Timestamp: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo ""
  echo "| Check | Status |"
  echo "|---|---|"
  for name in typecheck unit-tests-ts go-tests e2e-playwright visual-regression cerebro-patches bundle-budget; do
    status=$(get_result "$name")
    echo "| $name | ${status:-MISSING} |"
  done
  echo ""
  if $OVERALL_PASS; then
    echo "**Overall:** PASS — chunk eval gate green"
  else
    echo "**Overall:** FAIL — chunk eval gate red"
    echo ""
    echo "Failed check logs:"
    while IFS='|' read -r name status; do
      if [ "$status" = "FAIL" ]; then
        echo ""
        echo "## $name log"
        echo '```'
        tail -30 "/tmp/check_${name//[^a-zA-Z0-9]/_}.log" 2>/dev/null || echo "(log not found)"
        echo '```'
      fi
    done < "$RESULTS_FILE"
  fi
} > "$REPORT_FILE"

echo ""
echo "=== Eval report written to $REPORT_FILE ==="
cat "$REPORT_FILE" | head -20

if $OVERALL_PASS; then
  exit 0
else
  exit 1
fi

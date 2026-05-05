#!/usr/bin/env bash
# per-session-eval.sh — chunk-end eval gate
# Runs L3 evals consistently across autonomous-loop chunks
# Outputs pass/fail table + exit 0 (all pass) or exit 1 (any fail)

set -uo pipefail

CHUNK_ID="${1:-unknown}"
REPORT_DIR="docs/upstream-sync/eval-reports"
REPORT_FILE="${REPORT_DIR}/${CHUNK_ID}-$(date -u +%Y%m%dT%H%M%SZ).md"
mkdir -p "$REPORT_DIR"

declare -A RESULTS
OVERALL_PASS=true

run_check() {
  local name="$1"
  local cmd="$2"
  echo "=== Running: $name ==="
  if eval "$cmd" >/tmp/check_${name//[^a-zA-Z0-9]/_}.log 2>&1; then
    RESULTS[$name]="PASS"
    echo "[PASS] $name"
  else
    RESULTS[$name]="FAIL"
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

# L2 — Playwright E2E (only if dev server can be started)
# This is heavy; only run for chunks 7+ (Phase 2+) when full eval is needed
case "$CHUNK_ID" in
  chunk-7|chunk-8|chunk-9|chunk-10|chunk-11|chunk-12|phase-2|phase-3*|phase-4|phase-5)
    run_check "e2e-playwright" "pnpm exec playwright test --reporter=line"
    run_check "visual-regression" "pnpm exec playwright test e2e/visual.spec.ts --reporter=line"
    ;;
  *)
    RESULTS[e2e-playwright]="SKIP"
    RESULTS[visual-regression]="SKIP"
    ;;
esac

# Disciplin — CEREBRO-PATCH markers
if [ -f scripts/validate-cerebro-patches.sh ]; then
  run_check "cerebro-patches" "bash scripts/validate-cerebro-patches.sh"
else
  RESULTS[cerebro-patches]="SKIP (script not yet created)"
fi

# Bundle size budget (skip until baseline exists)
if [ -f e2e/budgets.json ]; then
  run_check "bundle-budget" "node scripts/measure-bundle.js"
else
  RESULTS[bundle-budget]="SKIP (baseline not yet captured)"
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
    echo "| $name | ${RESULTS[$name]:-MISSING} |"
  done
  echo ""
  if $OVERALL_PASS; then
    echo "**Overall:** PASS — chunk eval gate green"
  else
    echo "**Overall:** FAIL — chunk eval gate red"
    echo ""
    echo "Failed check logs:"
    for name in "${!RESULTS[@]}"; do
      if [ "${RESULTS[$name]}" = "FAIL" ]; then
        echo ""
        echo "## $name log"
        echo '```'
        tail -30 "/tmp/check_${name//[^a-zA-Z0-9]/_}.log" 2>/dev/null || echo "(log not found)"
        echo '```'
      fi
    done
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

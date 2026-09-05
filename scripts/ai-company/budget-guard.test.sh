#!/usr/bin/env bash
# Smoke test for budget-guard.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=lib/budget-guard.sh
source "$SCRIPT_DIR/lib/budget-guard.sh"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

export AUTOPILOT_BUDGET_DEFAULTS="$tmpdir/defaults.yaml"
export AUTOPILOT_BUDGET_STATE="$tmpdir/state.json"
cat >"$AUTOPILOT_BUDGET_DEFAULTS" <<'EOF'
budget:
  monthly_cursor_usd: 100
  alert_threshold_percent: 80
  pause_autopilot_on_exceed: true
EOF

if budget_guard_dispatch_allowed >/dev/null; then
  echo "ok: empty state allows dispatch"
else
  echo "fail: empty state should allow dispatch" >&2
  exit 1
fi

budget_guard_record_dispatch 40
export AUTOPILOT_MONTHLY_SPEND_USD=100
if budget_guard_dispatch_allowed >/dev/null 2>&1; then
  echo "fail: spend at cap should pause" >&2
  exit 1
fi
echo "ok: pause_autopilot_on_exceed blocks at cap"

unset AUTOPILOT_MONTHLY_SPEND_USD
export AUTOPILOT_BUDGET_STATE="$tmpdir/state2.json"
budget_guard_record_dispatch 10
if budget_guard_dispatch_allowed >/dev/null; then
  echo "ok: estimated spend below cap still allows"
else
  echo "fail: estimated spend below cap should allow" >&2
  exit 1
fi

echo "budget-guard.test.sh: all passed"

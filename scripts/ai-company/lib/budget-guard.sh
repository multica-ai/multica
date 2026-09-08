#!/usr/bin/env bash
# FinOps guard for portfolio autopilot — reads company-defaults.yaml + local state.
# No live Cursor billing API; uses AUTOPILOT_MONTHLY_SPEND_USD override or dispatch estimates.

budget_guard_defaults_path() {
  local root="${MULTICA_ROOT:-}"
  if [ -z "$root" ]; then
    root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
  fi
  echo "${AUTOPILOT_BUDGET_DEFAULTS:-$root/.ai-company/config/company-defaults.yaml}"
}

budget_guard_state_path() {
  echo "${AUTOPILOT_BUDGET_STATE:-${AUTOPILOT_STATE_DIR:-$HOME/.multica}/budget-state.json}"
}

# Exit 0 = dispatch allowed; 1 = paused (budget exceeded with pause_autopilot_on_exceed).
budget_guard_dispatch_allowed() {
  local defaults state
  defaults="$(budget_guard_defaults_path)"
  state="$(budget_guard_state_path)"
  python3 - "$defaults" "$state" <<'PY'
import json, os, sys
from datetime import datetime, timezone
from pathlib import Path

defaults_path = Path(sys.argv[1])
state_path = Path(sys.argv[2])
month_key = datetime.now().strftime("%Y-%m")

monthly = float(os.environ.get("AUTOPILOT_MONTHLY_BUDGET_USD", "0") or "0")
threshold = float(os.environ.get("AUTOPILOT_BUDGET_ALERT_PERCENT", "80") or "80")
pause_on_exceed = os.environ.get("AUTOPILOT_PAUSE_ON_EXCEED", "").lower() in ("1", "true", "yes")
estimated = float(os.environ.get("AUTOPILOT_ESTIMATED_USD_PER_DISPATCH", "3") or "3")
manual_spend = os.environ.get("AUTOPILOT_MONTHLY_SPEND_USD", "").strip()

if defaults_path.is_file():
    try:
        import yaml  # type: ignore
    except ImportError:
        yaml = None
    if yaml is not None:
        cfg = yaml.safe_load(defaults_path.read_text(encoding="utf-8")) or {}
        budget = cfg.get("budget") or {}
        if monthly <= 0:
            monthly = float(budget.get("monthly_cursor_usd") or 0)
        if threshold == 80 and budget.get("alert_threshold_percent") is not None:
            threshold = float(budget.get("alert_threshold_percent"))
        if not pause_on_exceed and budget.get("pause_autopilot_on_exceed") is True:
            pause_on_exceed = True
    else:
        text = defaults_path.read_text(encoding="utf-8")
        for line in text.splitlines():
            s = line.strip()
            if monthly <= 0 and s.startswith("monthly_cursor_usd:"):
                monthly = float(s.split(":", 1)[1].strip())
            if s.startswith("alert_threshold_percent:"):
                threshold = float(s.split(":", 1)[1].strip())
            if s.startswith("pause_autopilot_on_exceed:") and "true" in s.lower():
                pause_on_exceed = True

if monthly <= 0:
    sys.exit(0)

state = {}
if state_path.is_file():
    try:
        state = json.loads(state_path.read_text(encoding="utf-8"))
    except Exception:
        state = {}
entry = state.get(month_key) or {}
dispatches = int(entry.get("dispatches", 0))
if manual_spend:
    spend = float(manual_spend)
else:
    spend = float(entry.get("estimated_spend_usd", dispatches * estimated))

pct = (spend / monthly * 100) if monthly > 0 else 0
if pause_on_exceed and spend >= monthly:
    print(f"budget-paused spend={spend:.2f} monthly={monthly:.2f} pct={pct:.1f}")
    sys.exit(1)
if pct >= threshold:
    print(f"budget-warn spend={spend:.2f} monthly={monthly:.2f} pct={pct:.1f}")
sys.exit(0)
PY
}

budget_guard_record_dispatch() {
  local count="${1:-1}"
  local state estimated
  state="$(budget_guard_state_path)"
  estimated="${AUTOPILOT_ESTIMATED_USD_PER_DISPATCH:-3}"
  mkdir -p "$(dirname "$state")"
  python3 - "$state" "$count" "$estimated" <<'PY'
import json, sys
from datetime import datetime, timezone
from pathlib import Path

state_path = Path(sys.argv[1])
count = int(sys.argv[2])
estimated = float(sys.argv[3])
month_key = datetime.now().strftime("%Y-%m")

state = {}
if state_path.is_file():
    try:
        state = json.loads(state_path.read_text(encoding="utf-8"))
    except Exception:
        state = {}
entry = state.setdefault(month_key, {"dispatches": 0, "estimated_spend_usd": 0.0})
entry["dispatches"] = int(entry.get("dispatches", 0)) + count
entry["estimated_spend_usd"] = round(float(entry.get("estimated_spend_usd", 0)) + count * estimated, 2)
entry["updated_at"] = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
state_path.write_text(json.dumps(state, indent=2) + "\n", encoding="utf-8")
PY
}

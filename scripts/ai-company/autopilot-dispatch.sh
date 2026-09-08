#!/usr/bin/env bash
# Employee Autopilot — 值班经理：队列有粮且空闲时自动本地派单。
# 对齐：刘小排闭环默认转 + 硅谷 Owner/DoD/自动调度/升级矩阵。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=lib/source-local-env.sh
source "$SCRIPT_DIR/lib/source-local-env.sh"
# shellcheck source=lib/notify.sh
source "$SCRIPT_DIR/lib/notify.sh"
# shellcheck source=lib/agent-queue.sh
source "$SCRIPT_DIR/lib/agent-queue.sh"
# shellcheck source=lib/budget-guard.sh
source "$SCRIPT_DIR/lib/budget-guard.sh"

REGISTRY="${REGISTRY:-$MULTICA_ROOT/.ai-company/templates/project-registry.yaml}"
GITHUB_ORG="${GITHUB_ORG:-chenzh}"
MAX_TOTAL="${AUTOPILOT_MAX_TOTAL:-1}"
MAX_CONCURRENT="${AUTOPILOT_MAX_CONCURRENT:-1}"
MAX_BLOCKED="${AUTOPILOT_MAX_BLOCKED:-5}"
STATE_DIR="${AUTOPILOT_STATE_DIR:-$HOME/.multica}"
STATE_FILE="${AUTOPILOT_STATE_FILE:-$STATE_DIR/autopilot-state.json}"
LOG_DIR="${AUTOPILOT_LOG_DIR:-$HOME/.multica/autopilot-logs}"
DRY_RUN=0
FORCE=0
NOTIFY=1
QUIET_START="${AUTOPILOT_QUIET_START:-23}" # hour inclusive (Asia/Shanghai)
QUIET_END="${AUTOPILOT_QUIET_END:-6}"     # hour exclusive → quiet is [23, 24) U [0, 6)

usage() {
  cat <<'EOF'
Usage: autopilot-dispatch.sh [options]

Daytime employee autopilot:
  - Quiet hours 23:00–06:00 Asia/Shanghai → no dispatch (unless --force)
  - If QUEUE>0 and local RUNNING capacity available → portfolio-dispatch --local
  - Never blindly re-dispatch agent-blocked (portfolio pick_next_issue already skips)
  - After 2 consecutive runs with dispatch but QUEUE not decreasing → escalate via Feishu

Options:
  --dry-run         Decide only; do not dispatch / notify
  --force           Ignore quiet hours (for smoke / CEO override)
  --no-notify       Skip Feishu/Slack
  --max-total N     Dispatch cap this run (default: 2)
  --registry PATH
  --org ORG
  -h, --help

Env:
  AUTOPILOT_MAX_TOTAL / AUTOPILOT_MAX_CONCURRENT
  AUTOPILOT_QUIET_START / AUTOPILOT_QUIET_END (hours, Shanghai)
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) DRY_RUN=1; shift ;;
    --force) FORCE=1; shift ;;
    --no-notify) NOTIFY=0; shift ;;
    --max-total) MAX_TOTAL="${2:?}"; shift 2 ;;
    --registry) REGISTRY="${2:?}"; shift 2 ;;
    --org) GITHUB_ORG="${2:?}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

mkdir -p "$STATE_DIR" "$LOG_DIR"

if ! acquire_singleton_lock "autopilot-dispatch" "$STATE_DIR" 7200; then
  echo "another autopilot-dispatch is running — skip (singleton lock)"
  exit 0
fi

TS="$(date '+%Y%m%dT%H%M%S%z')"
LOG_FILE="$LOG_DIR/autopilot-$TS.log"
exec > >(tee -a "$LOG_FILE") 2>&1

echo "=== autopilot-dispatch $(date -Iseconds) ==="
echo "registry=$REGISTRY max_total=$MAX_TOTAL max_concurrent=$MAX_CONCURRENT dry_run=$DRY_RUN force=$FORCE"

shanghai_hour() {
  TZ=Asia/Shanghai date '+%H' | sed 's/^0*//;s/^$/0/'
}

in_quiet_hours() {
  local h
  h="$(shanghai_hour)"
  # 23..23 or 0..7
  if [ "$h" -ge "$QUIET_START" ] || [ "$h" -lt "$QUIET_END" ]; then
    return 0
  fi
  return 1
}

local_running_estimate() {
  local_dispatch_running_count
}

aggregate_queue() {
  # Sets: TOTAL_BLOCKED TOTAL_RUNNING TOTAL_QUEUE
  TOTAL_BLOCKED=0
  TOTAL_RUNNING=0
  TOTAL_QUEUE=0
  BLOCKED_LINES=""
  while IFS= read -r line; do
    [ -z "$line" ] && continue
    blocked="$(python3 -c "import json,sys; d=json.loads(sys.argv[1]); print(d.get('blocked',0))" "$line")"
    running="$(python3 -c "import json,sys; d=json.loads(sys.argv[1]); print(d.get('running',0))" "$line")"
    safe="$(python3 -c "import json,sys; d=json.loads(sys.argv[1]); print(d.get('agent_safe',0))" "$line")"
    paused="$(python3 -c "import json,sys; d=json.loads(sys.argv[1]); print(str(d.get('paused',False)).lower())" "$line")"
    id="$(python3 -c "import json,sys; d=json.loads(sys.argv[1]); print(d.get('id',''))" "$line")"
    repo="$(python3 -c "import json,sys; d=json.loads(sys.argv[1]); print(d.get('repo',''))" "$line")"
    if [ "$paused" = "true" ]; then
      continue
    fi
    TOTAL_BLOCKED=$((TOTAL_BLOCKED + blocked))
    TOTAL_RUNNING=$((TOTAL_RUNNING + running))
    TOTAL_QUEUE=$((TOTAL_QUEUE + safe))
    if [ "$blocked" -gt 0 ]; then
      BLOCKED_LINES="${BLOCKED_LINES}"$'\n'"- $id ($repo): BLOCKED=$blocked"
    fi
  done < <(bash "$SCRIPT_DIR/ceo-dashboard.sh" --registry "$REGISTRY" --org "$GITHUB_ORG" --json 2>/dev/null || true)
}

load_state_vars() {
  PREV_QUEUE=0
  PREV_DISPATCHED=0
  CONSEC=0
  PREV_ESCALATE=""
  if [ -f "$STATE_FILE" ]; then
    eval "$(python3 - "$STATE_FILE" <<'PY'
import json, sys
from pathlib import Path
try:
    d = json.loads(Path(sys.argv[1]).read_text())
except Exception:
    d = {}
esc = (d.get("lastEscalateAt") or "").replace("\\", "").replace('"', "")
print(f"PREV_QUEUE={int(d.get('lastQueue', 0))}")
print(f"PREV_DISPATCHED={int(d.get('lastDispatched', 0))}")
print(f"CONSEC={int(d.get('consecutiveNoProgress', 0))}")
print(f'PREV_ESCALATE="{esc}"')
PY
)"
  fi
}

save_state() {
  local last_queue="$1" last_dispatched="$2" consec="$3" escalate_at="$4"
  python3 - "$STATE_FILE" "$last_queue" "$last_dispatched" "$consec" "$escalate_at" <<'PY'
import json, sys
from pathlib import Path
from datetime import datetime, timezone
path = Path(sys.argv[1])
d = {
  "lastQueue": int(sys.argv[2]),
  "lastDispatched": int(sys.argv[3]),
  "consecutiveNoProgress": int(sys.argv[4]),
  "lastEscalateAt": sys.argv[5],
  "lastRunAt": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
}
path.write_text(json.dumps(d, indent=2) + "\n")
PY
}

maybe_notify() {
  local text="$1"
  if [ "$NOTIFY" -eq 0 ] || [ "$DRY_RUN" -eq 1 ]; then
    echo "[notify skipped] $text"
    return 0
  fi
  if ! has_ceo_notify_channel; then
    echo "[notify] no channel configured — $text"
    return 0
  fi
  notify_ceo_brief "$text" || echo "warn: notify failed" >&2
}

# --- quiet hours ---
if [ "$FORCE" -eq 0 ] && in_quiet_hours; then
  echo "quiet hours (Asia/Shanghai ${QUIET_START}:00–${QUIET_END}:00) — skip dispatch"
  echo "log=$LOG_FILE"
  exit 0
fi

if [ "$DRY_RUN" -eq 0 ]; then
  cleaned="$(cleanup_stale_local_dispatches 0 || echo 0)"
  echo "stale dispatch cleanup: removed/killed ${cleaned:-0}"
  bash "$SCRIPT_DIR/ceo-reconcile-queue.sh" --registry "$REGISTRY" --org "$GITHUB_ORG" 2>/dev/null || true
  if [ "${CEO_AUTO_MERGE:-1}" = "1" ]; then
    echo ">> auto-merge green PRs (autopilot)"
    bash "$SCRIPT_DIR/ceo-auto-merge.sh" \
      --registry "$REGISTRY" \
      --org "$GITHUB_ORG" 2>/dev/null || echo "warn: auto-merge failed (continuing)" >&2
  fi
fi

aggregate_queue
LOCAL_PROCS="$(local_running_estimate)"
echo "TOTAL BLOCKED=$TOTAL_BLOCKED RUNNING_LABELS=$TOTAL_RUNNING QUEUE=$TOTAL_QUEUE local_cli≈$LOCAL_PROCS"

load_state_vars

ACTION="idle"
DISPATCHED=0
SLOTS=0
ESCALATE_AT="${PREV_ESCALATE:-}"

if [ "$TOTAL_QUEUE" -le 0 ]; then
  ACTION="idle-empty"
  echo "decision: queue empty — no dispatch"
  if [ "$TOTAL_BLOCKED" -gt 0 ]; then
    ACTION="blocked-only"
    msg="🔴 Autopilot：无可用 QUEUE，但有 BLOCKED=$TOTAL_BLOCKED（不盲派）。
$BLOCKED_LINES

请处理：runbooks/blocked-triage.md
日志：$LOG_FILE"
    maybe_notify "$msg"
  fi
else
  busy="$TOTAL_RUNNING"
  if [ "$LOCAL_PROCS" -gt "$busy" ]; then
    busy="$LOCAL_PROCS"
  fi
  if [ "$TOTAL_BLOCKED" -ge "$MAX_BLOCKED" ]; then
    ACTION="blocked-backpressure"
    echo "decision: blocked backpressure (blocked=$TOTAL_BLOCKED >= $MAX_BLOCKED) — skip dispatch; run reconcile first"
    maybe_notify "🔴 Autopilot 背压：BLOCKED=$TOTAL_BLOCKED（≥$MAX_BLOCKED）。先清 blocked / 假 running 再派单。
bash scripts/ai-company/ceo-reconcile-queue.sh
log: $LOG_FILE"
  elif [ "$busy" -ge "$MAX_CONCURRENT" ]; then
    ACTION="busy"
    echo "decision: at capacity (busy=$busy >= $MAX_CONCURRENT) — no new dispatch"
  elif ! budget_guard_dispatch_allowed; then
    ACTION="budget-paused"
    echo "decision: monthly budget exceeded — pause_autopilot_on_exceed (set AUTOPILOT_MONTHLY_SPEND_USD or edit budget-state.json)"
    maybe_notify "🔴 Autopilot FinOps 暂停：本月 Cursor 预算已用尽（pause_autopilot_on_exceed）。
覆盖：export AUTOPILOT_MONTHLY_SPEND_USD=… 或调 company-defaults.yaml
状态：$(budget_guard_state_path)
log: $LOG_FILE"
  else
    SLOTS=$((MAX_CONCURRENT - busy))
    if [ "$SLOTS" -gt "$MAX_TOTAL" ]; then
      SLOTS="$MAX_TOTAL"
    fi
    if [ "$SLOTS" -gt "$TOTAL_QUEUE" ]; then
      SLOTS="$TOTAL_QUEUE"
    fi
    ACTION="dispatch"
    echo "decision: dispatch up to $SLOTS (local-cli, background)"
    if [ "$DRY_RUN" -eq 1 ]; then
      echo "[dry-run] would run: portfolio-dispatch.sh --local --max-total $SLOTS"
      DISPATCHED="$SLOTS"
    else
      # Fire-and-forget：值班经理只负责派活，不阻塞等 Agent 跑完（与 ceo-nightly BG 一致）
      BG_LOG="$LOG_DIR/portfolio-bg-$TS.log"
      nohup bash "$SCRIPT_DIR/portfolio-dispatch.sh" \
        --registry "$REGISTRY" \
        --local \
        --max-total "$SLOTS" \
        >>"$BG_LOG" 2>&1 &
      bg_pid=$!
      echo "portfolio-dispatch started pid=$bg_pid log=$BG_LOG"
      DISPATCHED="$SLOTS"
      budget_guard_record_dispatch "$DISPATCHED" || true
      # Brief wait so agent-running labels / CLI start show up in logs
      sleep 8 || true
      if [ -n "${bg_pid:-}" ] && ! kill -0 "$bg_pid" 2>/dev/null; then
        wait "$bg_pid" 2>/dev/null || true
        DISPATCHED="$(grep -o 'Planned/dispatched task slots: [0-9][0-9]*' "$BG_LOG" 2>/dev/null | tail -1 | grep -o '[0-9][0-9]*$' || echo "$SLOTS")"
        echo "portfolio-dispatch finished early dispatched≈$DISPATCHED"
      fi
    fi
  fi
fi

# --- spin detection ---
if [ "$ACTION" = "dispatch" ] && [ "${DISPATCHED:-0}" -gt 0 ]; then
  if [ "${PREV_DISPATCHED:-0}" -gt 0 ] && [ "$TOTAL_QUEUE" -ge "${PREV_QUEUE:-0}" ] && [ "${PREV_QUEUE:-0}" -gt 0 ]; then
    CONSEC=$((CONSEC + 1))
  else
    CONSEC=0
  fi
fi

if [ "${CONSEC:-0}" -ge 2 ]; then
  msg="⚠️ Autopilot 空转告警：连续 ${CONSEC} 次派单后 QUEUE 未下降（现 QUEUE=${TOTAL_QUEUE}，上次=${PREV_QUEUE}）。
请检查：并发撞车、worktree 失败、缺 DoD、或票永远 BLOCKED。
日志：${LOG_FILE}
仪表盘：bash scripts/ai-company/ceo-dashboard.sh"
  maybe_notify "$msg"
  ESCALATE_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  CONSEC=0
fi

# Notify on actual dispatch (not dry-run noise every hour if zero)
if [ "$ACTION" = "dispatch" ] && [ "${DISPATCHED:-0}" -gt 0 ] && [ "$DRY_RUN" -eq 0 ]; then
  maybe_notify "Autopilot dispatched ~${DISPATCHED} (QUEUE was ${TOTAL_QUEUE:-0}, RUNNING~${TOTAL_RUNNING:-0}, cap ${MAX_CONCURRENT}).
log: $LOG_FILE"
fi

save_state "${TOTAL_QUEUE:-0}" "${DISPATCHED:-0}" "${CONSEC:-0}" "${ESCALATE_AT:-}"

echo "summary action=$ACTION queue=${TOTAL_QUEUE:-0} blocked=${TOTAL_BLOCKED:-0} dispatched=${DISPATCHED:-0} consec=${CONSEC:-0}"
echo "state=$STATE_FILE"
echo "log=$LOG_FILE"

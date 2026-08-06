#!/bin/bash
# Tests the shutdown drain/escalation logic in entrypoint.sh.
#
# Which signal the entrypoint sends decides whether an interrupted agent session
# survives a pod roll: SIGTERM makes the daemon report the task
# failure_reason="cancelled" (not retryable — work lost), while SIGKILL leaves it
# `running` for the next pod's recover-orphans to reclaim as "runtime_recovery"
# (retryable — resumes with session_id + work_dir). That asymmetry is easy to
# regress silently, hence these tests.
#
# The functions under test are extracted from entrypoint.sh rather than
# duplicated, so the tests exercise the shipped code. Run: ./entrypoint-drain.test.sh
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENTRYPOINT="${SCRIPT_DIR}/entrypoint.sh"

failures=0
pass() { echo "  ok   — $1"; }
fail() { echo "  FAIL — $1" >&2; failures=$((failures + 1)); }

# ── Load the functions under test ─────────────────────────────────────────────
# The region from `active_task_count() {` to the line before `trap` holds exactly
# the two shutdown helpers and nothing else.
extract_drain_functions() {
  sed -n '/^active_task_count() {$/,/^trap /p' "${ENTRYPOINT}" | sed '/^trap /d'
}

drain_src="$(extract_drain_functions)"
if ! grep -q 'active_task_count() {' <<<"${drain_src}" || ! grep -q 'drain_and_stop() {' <<<"${drain_src}"; then
  echo "FATAL: could not extract drain functions from ${ENTRYPOINT}" >&2
  echo "       (did the function names or the trap line change?)" >&2
  exit 1
fi
eval "${drain_src}"

# ── Harness ───────────────────────────────────────────────────────────────────
WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"; [[ -n "${DAEMON_PID:-}" ]] && kill -KILL "${DAEMON_PID}" 2>/dev/null' EXIT
PATH="${WORK}/bin:${PATH}"
mkdir -p "${WORK}/bin"

# Stub `multica daemon status --output json`. Emits the lines of
# ${WORK}/responses one per invocation, repeating the last one once exhausted —
# so a test can script "busy, busy, then idle".
cat > "${WORK}/bin/multica" <<'STUB'
#!/bin/bash
resp="${STUB_RESPONSES}"
n=$(cat "${STUB_CALLS}" 2>/dev/null || echo 0)
echo $((n + 1)) > "${STUB_CALLS}"
total=$(wc -l < "${resp}")
line=$((n + 1))
(( line > total )) && line="${total}"
sed -n "${line}p" "${resp}"
STUB
chmod +x "${WORK}/bin/multica"

export STUB_RESPONSES="${WORK}/responses"
export STUB_CALLS="${WORK}/calls"

# Scripts the stub daemon's status replies, one JSON doc per line.
set_responses() { printf '%s\n' "$@" > "${STUB_RESPONSES}"; : > "${STUB_CALLS}"; }

SIGLOG=""
# Background stand-in for the daemon. `catchable` records SIGTERM then exits;
# `ignores_term` blocks SIGTERM so the escalation path can be exercised.
start_stub_daemon() {
  local mode="${1:-catchable}"
  SIGLOG="${WORK}/signals.$$.${RANDOM}"
  : > "${SIGLOG}"
  if [[ "${mode}" == "ignores_term" ]]; then
    ( trap '' SIGTERM; while :; do sleep 0.2; done ) &
  else
    ( trap 'echo TERM >> "'"${SIGLOG}"'"; exit 0' SIGTERM; while :; do sleep 0.2; done ) &
  fi
  DAEMON_PID=$!
}

daemon_alive() { kill -0 "${DAEMON_PID}" 2>/dev/null; }
got_sigterm()  { grep -q TERM "${SIGLOG}" 2>/dev/null; }

BUSY='{"status":"running","active_task_count":2}'
IDLE='{"status":"running","active_task_count":0}'
DOWN='{"status":"stopped"}'

# ── Tests ─────────────────────────────────────────────────────────────────────

echo "idle daemon stops cleanly with SIGTERM"
set_responses "${IDLE}"
start_stub_daemon catchable
DRAIN_MAX_SECONDS=10 DAEMON_STOP_WAIT_SECONDS=5 drain_and_stop >/dev/null 2>&1
sleep 0.4
got_sigterm  && pass "SIGTERM delivered" || fail "expected SIGTERM, daemon never saw one"
daemon_alive && fail "daemon still alive after drain returned" || pass "daemon reaped before return"

echo "busy daemon past the cap is SIGKILLed, not SIGTERMed"
set_responses "${BUSY}"
start_stub_daemon catchable
start=${SECONDS}
DRAIN_MAX_SECONDS=5 DAEMON_STOP_WAIT_SECONDS=5 drain_and_stop >/dev/null 2>&1
elapsed=$((SECONDS - start))
sleep 0.4
got_sigterm  && fail "sent SIGTERM to a busy daemon — the interrupted task would be unrecoverable" \
             || pass "no SIGTERM sent"
daemon_alive && fail "daemon survived" || pass "daemon killed"
(( elapsed >= 5 )) && pass "waited out the ${elapsed}s drain window" \
                   || fail "returned after ${elapsed}s, expected to drain for at least 5s"

echo "daemon that goes idle mid-drain gets the clean stop"
set_responses "${BUSY}" "${BUSY}" "${IDLE}"
start_stub_daemon catchable
DRAIN_MAX_SECONDS=60 DAEMON_STOP_WAIT_SECONDS=5 drain_and_stop >/dev/null 2>&1
sleep 0.4
got_sigterm && pass "SIGTERM delivered once idle" || fail "expected SIGTERM after daemon reported idle"

echo "unreadable status bails out early and takes the recoverable stop"
set_responses "${DOWN}"
start_stub_daemon catchable
start=${SECONDS}
DRAIN_MAX_SECONDS=600 DAEMON_STOP_WAIT_SECONDS=5 drain_and_stop >/dev/null 2>&1
elapsed=$((SECONDS - start))
sleep 0.4
got_sigterm && fail "treated an unreachable daemon as idle and sent SIGTERM" \
            || pass "no SIGTERM on unknown state"
(( elapsed < 60 )) && pass "gave up after ${elapsed}s instead of burning the 600s cap" \
                   || fail "spent ${elapsed}s on an unreachable daemon"

echo "a daemon ignoring SIGTERM is escalated to SIGKILL"
set_responses "${IDLE}"
start_stub_daemon ignores_term
DRAIN_MAX_SECONDS=10 DAEMON_STOP_WAIT_SECONDS=3 drain_and_stop >/dev/null 2>&1
daemon_alive && fail "daemon outlived drain_and_stop — the pod would hang until the grace period" \
             || pass "escalated and reaped"

echo
if (( failures > 0 )); then
  echo "${failures} failure(s)" >&2
  exit 1
fi
echo "all drain tests passed"

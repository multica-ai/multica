#!/usr/bin/env bash
set -euo pipefail

# shellcheck disable=SC1091
. scripts/check-process-tree.sh

bash -c 'sleep 300 & wait' 2>/dev/null &
root_pid=$!
sleep 0.1
child_pid="$(pgrep -P "$root_pid")"

stop_check_process_tree "$root_pid"

process_is_running() {
  local pid=$1 state
  kill -0 "$pid" 2>/dev/null || return 1
  state="$(ps -o stat= -p "$pid" 2>/dev/null | tr -d '[:space:]')"
  [ -n "$state" ] && [[ "$state" != Z* ]]
}

# Linux can briefly retain an already-terminated child as a zombie while its
# parent exits. A zombie cannot keep check.sh alive and is therefore stopped.
if process_is_running "$root_pid" || process_is_running "$child_pid"; then
  echo "check process tree test: process survived cleanup" >&2
  exit 1
fi

echo "check process tree tests: PASS"

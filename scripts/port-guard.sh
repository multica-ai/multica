#!/usr/bin/env bash
# Listener-only port helpers shared by `make stop`, scripts/dev.sh, and
# scripts/dev-bootstrap.sh.
#
# The rule every command here enforces: only a process LISTENING on the port is
# ours to inspect or stop. Bare `lsof -ti:PORT` also matches every client
# connected to it — including the local daemon, which holds a long-lived
# connection to the backend — so stopping the backend that way silently kills
# the daemon too (its log just stops mid-heartbeat). `-sTCP:LISTEN` is the whole
# fix, and it is why these calls live in one place instead of being inlined.
#
# Usage:
#   port-guard.sh listeners <port>                 # print listening PIDs, one per line
#   port-guard.sh require-free <port> [label]      # exit 1 if the port is taken
#   port-guard.sh stop <port> [label]              # TERM, then KILL what is left
set -euo pipefail

# Prints the PIDs listening on a port, nothing else. Empty output (exit 0) means
# the port is free, or that lsof is unavailable to tell us otherwise.
port_listeners() {
  local port=$1
  command -v lsof > /dev/null 2>&1 || return 0
  lsof -ti:"$port" -sTCP:LISTEN 2>/dev/null || true
}

# Describes who owns a port, for error messages: "1234 (node, started Thu ...)".
port_owner_description() {
  local pid=$1 proc_name started_at
  proc_name="$(ps -o comm= -p "$pid" 2>/dev/null | tr -d ' ' || true)"
  started_at="$(ps -o lstart= -p "$pid" 2>/dev/null | sed 's/^ *//' || true)"
  if [ -n "$proc_name" ] && [ -n "$started_at" ]; then
    echo "$pid ($proc_name, started $started_at)"
  elif [ -n "$proc_name" ]; then
    echo "$pid ($proc_name)"
  else
    echo "$pid"
  fi
}

cmd_listeners() {
  port_listeners "$1"
}

# Fails loudly when a port is already served. Without this a second `make dev`
# dies on the bind while the OLD process keeps answering /health with 200, so
# the restart looks healthy and you keep testing the pre-fix binary.
cmd_require_free() {
  local port=$1 label=${2:-service} pids pid
  pids="$(port_listeners "$port")"
  [ -n "$pids" ] || return 0

  echo "✗ Port $port ($label) is already in use:" >&2
  while IFS= read -r pid; do
    [ -n "$pid" ] || continue
    echo "    $(port_owner_description "$pid")" >&2
  done <<< "$pids"
  echo "" >&2
  echo "  A second instance cannot bind it, and the process above would keep serving" >&2
  echo "  requests — including a healthy /health — so the restart would look fine while" >&2
  echo "  the old build stayed live. Stop it first:" >&2
  echo "    make stop            # main checkout" >&2
  echo "    make stop-worktree   # worktree checkout" >&2
  return 1
}

# Stops only the listeners: TERM first so the process can shut down cleanly,
# KILL only for whatever is still alive after the grace period.
cmd_stop() {
  local port=$1 label=${2:-service} pids pid
  pids="$(port_listeners "$port")"
  if [ -z "$pids" ]; then
    echo "    $label (:$port) was not running."
    return 0
  fi

  while IFS= read -r pid; do
    [ -n "$pid" ] || continue
    kill -TERM "$pid" 2> /dev/null || true
  done <<< "$pids"

  local waited=0
  while [ "$waited" -lt 10 ]; do
    [ -n "$(port_listeners "$port")" ] || break
    sleep 0.5
    waited=$((waited + 1))
  done

  local remaining
  remaining="$(port_listeners "$port")"
  if [ -n "$remaining" ]; then
    while IFS= read -r pid; do
      [ -n "$pid" ] || continue
      kill -9 "$pid" 2> /dev/null || true
    done <<< "$remaining"
    echo "    $label (:$port) did not exit on TERM; killed."
  else
    echo "    $label (:$port) stopped."
  fi
}

# Warn once rather than per lookup: without lsof every command below degrades to
# "the port looks free", and a silent no-op stop is worse than a loud one.
if ! command -v lsof > /dev/null 2>&1 && [ "${1:-}" != listeners ]; then
  echo "note: lsof not found — cannot inspect port ownership; skipping this check." >&2
fi

case "${1:-}" in
  listeners) shift; cmd_listeners "$@" ;;
  require-free) shift; cmd_require_free "$@" ;;
  stop) shift; cmd_stop "$@" ;;
  *)
    echo "Usage: port-guard.sh {listeners|require-free|stop} <port> [label]" >&2
    exit 2
    ;;
esac

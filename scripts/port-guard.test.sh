#!/usr/bin/env bash
# Verifies scripts/port-guard.sh against a real listener and a real connected
# client. The client half is the regression this file exists for: `make stop`
# used to run bare `lsof -ti:PORT`, which matches clients as well as the
# listener, so stopping the backend also killed the daemon connected to it.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

GUARD="scripts/port-guard.sh"

if ! command -v lsof > /dev/null 2>&1; then
  echo "lsof unavailable — skipping port-guard tests."
  exit 0
fi

PORT="${PORT_GUARD_TEST_PORT:-19741}"
LISTENER_PID=""
CLIENT_PID=""

cleanup() {
  [ -n "$CLIENT_PID" ] && kill -9 "$CLIENT_PID" 2> /dev/null || true
  [ -n "$LISTENER_PID" ] && kill -9 "$LISTENER_PID" 2> /dev/null || true
}
trap cleanup EXIT

fail() {
  echo "✗ $1" >&2
  exit 1
}

# node is already a hard prerequisite of the local dev flow (scripts/dev.sh), so
# using it for the fixtures keeps this test dependency-free.
node -e '
  const net = require("net");
  net.createServer(s => s.on("error", () => {})).listen(Number(process.argv[1]), "127.0.0.1");
  setTimeout(() => process.exit(0), 60000);
' "$PORT" &
LISTENER_PID=$!
# Drop the job from the table so bash does not print "Terminated" noise when the
# fixture is stopped as part of the assertions below.
disown "$LISTENER_PID" 2> /dev/null || true

for _ in $(seq 1 40); do
  [ -n "$(bash "$GUARD" listeners "$PORT")" ] && break
  sleep 0.25
done

listeners="$(bash "$GUARD" listeners "$PORT")"
[ -n "$listeners" ] || fail "listener on :$PORT never came up"

# A client connected to the same port. Bare `lsof -ti:PORT` reports this PID
# too; `listeners` must not.
node -e '
  const net = require("net");
  const socket = net.connect(Number(process.argv[1]), "127.0.0.1");
  socket.on("error", () => {});
  setTimeout(() => process.exit(0), 60000);
' "$PORT" &
CLIENT_PID=$!
disown "$CLIENT_PID" 2> /dev/null || true
sleep 1

if ! kill -0 "$CLIENT_PID" 2> /dev/null; then
  fail "client fixture did not stay connected"
fi

# Prove the fixture reproduces the original hazard before asserting the fix:
# the unfiltered lookup `make stop` used to run does match the client.
if ! grep -Fxq "$CLIENT_PID" <<< "$(lsof -ti:"$PORT" 2>/dev/null || true)"; then
  fail "unfiltered lsof did not see the client — fixture cannot prove the filter matters"
fi

if grep -Fxq "$CLIENT_PID" <<< "$(bash "$GUARD" listeners "$PORT")"; then
  fail "listeners reported the connected client (pid $CLIENT_PID) — -sTCP:LISTEN is missing"
fi

if bash "$GUARD" require-free "$PORT" backend > /dev/null 2>&1; then
  fail "require-free succeeded on an occupied port"
fi

require_free_output="$(bash "$GUARD" require-free "$PORT" backend 2>&1 || true)"
grep -q "make stop" <<< "$require_free_output" \
  || fail "require-free must tell the caller how to stop the running instance"

bash "$GUARD" stop "$PORT" backend > /dev/null

if kill -0 "$LISTENER_PID" 2> /dev/null; then
  fail "stop left the listener (pid $LISTENER_PID) running"
fi
if ! kill -0 "$CLIENT_PID" 2> /dev/null; then
  fail "stop killed the connected client (pid $CLIENT_PID) — this is the daemon-kill bug"
fi

bash "$GUARD" require-free "$PORT" backend > /dev/null \
  || fail "require-free failed on a port that is now free"

bash "$GUARD" stop "$PORT" backend | grep -q "was not running" \
  || fail "stop on a free port should report that nothing was running"

echo "✓ port-guard.sh: listener-only detection, stop, and preflight all behave."

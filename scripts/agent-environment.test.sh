#!/usr/bin/env bash
# Lifecycle of scripts/agent-environment.sh against a stub dev-env.sh: who
# stops a component, who destroys the shared environment, and that exactly one
# of them does whatever order or timing the stops come in.
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_dir="$(mktemp -d)"
pids=()
cleanup() {
  local pid
  for pid in ${pids[@]+"${pids[@]}"}; do kill "$pid" 2>/dev/null || true; done
  wait 2>/dev/null || true
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

fail() {
  echo "FAIL: $*" >&2
  [ ! -f "$repo/calls.log" ] || sed 's/^/  calls: /' "$repo/calls.log" >&2
  exit 1
}

free_port() {
  node -e 'const s = require("node:net").createServer().listen(0, "127.0.0.1", () => { console.log(s.address().port); s.close(); });'
}

# A fresh fake checkout: the real script next to a dev-env.sh that records
# each verb and keeps "registered" as the environment's existence.
new_repo() {
  repo="$tmp_dir/repo-$1"
  mkdir -p "$repo/scripts"
  cp "$root_dir/scripts/agent-environment.sh" "$repo/scripts/"
  component_port="$(free_port)"
  cat > "$repo/scripts/dev-env.sh" <<EOF
#!/usr/bin/env bash
set -eu
cd "$repo"
case "\$1" in
  up) echo "up \$*" >> calls.log; touch registered ;;
  status)
    [ -f registered ] || exit 1
    printf '{"frontend_port":%s,"desktop_renderer_port":%s,"workspace_slug":"dev"}\n' "$component_port" "$component_port" ;;
  down) sleep 0.3; echo "down \$3" >> calls.log ;;
  destroy) sleep 0.3; echo destroy >> calls.log; rm -f registered ;;
esac
EOF
  : > "$repo/calls.log"
}

count() { grep -cx "$1" "$repo/calls.log" || true; }

# A process that stands in for a running start: its pid is what a claim names.
live_claim() {
  mkdir -p "$repo/.multica/agent-environments"
  sleep 300 &
  pids+=("$!")
  printf '%s\n' "$!" > "$repo/.multica/agent-environments/$1.claim"
}

# Kills and reaps a background job, so bash does not report it as terminated.
stop_job() {
  kill "$1" 2>/dev/null || true
  wait "$1" 2>/dev/null || true
}

serve_component() {
  node -e 'require("node:http").createServer((_, res) => res.end("ok")).listen(Number(process.argv[1]), "127.0.0.1")' "$component_port" &
  component_pid=$!
  pids+=("$component_pid")
}

start_in_background() {
  MULTICA_PORT="$(free_port)" bash "$repo/scripts/agent-environment.sh" start "$1" > "$repo/start.log" 2>&1 &
  start_pid=$!
  pids+=("$start_pid")
  local i
  for i in $(seq 1 100); do
    grep -q "redirects to\|watching its renderer" "$repo/start.log" 2>/dev/null && return 0
    sleep 0.1
  done
  fail "start $1 never came up: $(cat "$repo/start.log")"
}

wait_for_exit() {
  local i
  for i in $(seq 1 300); do
    kill -0 "$1" 2>/dev/null || { wait "$1" 2>/dev/null || true; return 0; }
    sleep 0.1
  done
  fail "process $1 did not exit"
}

# 1. The component going away is cleaned up by start itself: the daemon
#    neither runs stop nor expires an environment whose start has exited.
new_repo exit-last
serve_component
start_in_background web
stop_job "$component_pid"
wait_for_exit "$start_pid"
[ "$(count "down web")" = 1 ] && [ "$(count destroy)" = 1 ] || fail "a component that exited was not cleaned up"
[ ! -f "$repo/registered" ] || fail "the environment outlived its last component"

# 2. ...but what the other component still uses is kept.
new_repo exit-shared
live_claim web
serve_component
start_in_background desktop
stop_job "$component_pid"
wait_for_exit "$start_pid"
[ "$(count "down desktop")" = 1 ] && [ "$(count destroy)" = 0 ] || fail "an exiting component destroyed what web still uses"

# 3. Two stops at once: both components stop, exactly one destroys.
new_repo concurrent
touch "$repo/registered"
live_claim web
live_claim desktop
bash "$repo/scripts/agent-environment.sh" stop web > /dev/null &
first=$!
bash "$repo/scripts/agent-environment.sh" stop desktop > /dev/null &
second=$!
wait "$first" || fail "stop web failed"
wait "$second" || fail "stop desktop failed"
[ "$(count "down web")" = 1 ] && [ "$(count "down desktop")" = 1 ] || fail "concurrent stops did not stop both components"
[ "$(count destroy)" = 1 ] || fail "concurrent stops destroyed $(count destroy) times, want once"

# 4. The daemon signals start after running stop. start's own cleanup then
#    finds nothing left to do.
new_repo signalled
serve_component
start_in_background web
bash "$repo/scripts/agent-environment.sh" stop web > /dev/null
stop_job "$start_pid"
[ "$(count "down web")" = 1 ] && [ "$(count destroy)" = 1 ] || fail "stop followed by the daemon's signal cleaned up more than once"

# 5. A claim whose start was killed outright does not keep the environment.
new_repo stale
touch "$repo/registered"
mkdir -p "$repo/.multica/agent-environments"
sleep 0 & dead=$!
wait "$dead"
printf '%s\n' "$dead" > "$repo/.multica/agent-environments/desktop.claim"
bash "$repo/scripts/agent-environment.sh" stop web > /dev/null
[ "$(count destroy)" = 1 ] || fail "a stale claim kept the environment alive"

echo "✓ agent-environment.sh lifecycle verified"

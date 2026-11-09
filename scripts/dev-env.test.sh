#!/usr/bin/env bash
# Registry-level behaviour of scripts/dev-env.sh, with no services started.
#
# Everything here runs against a throwaway MULTICA_DEV_HOME holding hand-written
# manifests, so the verbs are exercised end to end without a database, a
# backend, or a port.
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

export MULTICA_DEV_HOME="$tmp_dir/dev"
export MULTICA_DEV_WORKSPACES_PARENT="$tmp_dir/workspaces-parent"
export MULTICA_DEV_DESKTOP_APP_DATA="$tmp_dir/app-data"
export MULTICA_DEV_PROFILES_HOME="$tmp_dir/profiles"

fake_bin="$tmp_dir/bin"
mkdir -p "$fake_bin"
cat > "$fake_bin/psql" <<'EOF'
#!/usr/bin/env bash
case " $* " in
  *" DROP DATABASE "*) [ "${FAIL_DROP:-0}" != 1 ] ;;
  *) printf '1\n' ;;
esac
EOF
chmod +x "$fake_bin/psql"
export PATH="$fake_bin:$PATH"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

require_contains() {
  local file=$1 expected=$2
  if ! grep -Fq "$expected" "$file"; then
    echo "Expected output to contain: $expected" >&2
    echo "Observed:" >&2
    sed 's/^/  /' "$file" >&2
    exit 1
  fi
}

dev_env() {
  bash "$root_dir/scripts/dev-env.sh" "$@"
}

write_manifest() {
  local name=$1 dir=$2 offset=$3
  local profile="dev-dev-env-test-$offset"
  mkdir -p "$MULTICA_DEV_HOME/envs/$name/logs"
  cat > "$MULTICA_DEV_HOME/envs/$name/manifest.env" <<EOF
NAME=$name
DIR=$(printf '%q' "$dir")
CREATED_AT=2026-01-01T00:00:00Z
OWNER=agent
TTL_HOURS=0
ENV_FILE=.env.example
OFFSET=$offset
BACKEND_PORT=$((18080 + offset))
FRONTEND_PORT=$((13000 + offset))
DB_NAME=multica_dev_env_test_$offset
DATABASE_URL=postgres://multica:multica@localhost:5432/multica_dev_env_test_$offset?sslmode=disable
PROFILE=$profile
WORKSPACES_ROOT=$(printf '%q' "$MULTICA_DEV_WORKSPACES_PARENT/multica_workspaces_$profile")
DESKTOP_RENDERER_PORT=$((5174 + offset))
DESKTOP_APP_SUFFIX=$name
EOF
}

out="$tmp_dir/out"

assert_listener_ownership() {
  local case_name=$1 expected=$2 launcher=$3 listener=$4 listener_pgid=$5 recorded=${6:-}
  (
    # shellcheck disable=SC1090
    source "$root_dir/scripts/dev-env.sh"
    STATE_DIR="$tmp_dir/ownership-$case_name"
    mkdir -p "$STATE_DIR"
    [ -z "$recorded" ] || printf '%s\n' "$recorded" > "$(listener_pid_file web)"
    TEST_LAUNCHER=$launcher
    TEST_LISTENER=$listener
    TEST_LISTENER_PGID=$listener_pgid
    TEST_CASE=$case_name

    component_pid() { printf '%s' "$TEST_LAUNCHER"; }
    port_listener_pid() { printf '%s' "$TEST_LISTENER"; }
    process_group_id() { printf '%s' "$TEST_LISTENER_PGID"; }
    process_parent_id() {
      case "$TEST_CASE:$1" in
        nested:420) printf '310' ;;
        nested:310) printf '200' ;;
        nested:200) printf '%s' "$TEST_LAUNCHER" ;;
        *) printf '1' ;;
      esac
    }

    local actual=external
    if listener_belongs_to_component web 13000; then actual=owned; fi
    [ "$actual" = "$expected" ] \
      || fail "$case_name listener ownership = $actual, want $expected"
  )
}

assert_nested_listener_is_recorded() (
  # shellcheck disable=SC1090
  source "$root_dir/scripts/dev-env.sh"
  STATE_DIR="$tmp_dir/ownership-record"
  mkdir -p "$STATE_DIR"

  component_pid() { printf '100'; }
  port_listener_pid() { printf '420'; }
  process_group_id() { printf '310'; }
  process_parent_id() {
    case "$1" in
      420) printf '310' ;;
      310) printf '200' ;;
      200) printf '100' ;;
      *) printf '1' ;;
    esac
  }

  local claimed
  claimed="$(record_component_listener web 13000)" \
    || fail "nested listener was not claimed"
  [ "$claimed" = 420 ] || fail "claimed listener = $claimed, want 420"
  [ "$(cat "$(listener_pid_file web)")" = 420 ] \
    || fail "nested listener pid was not recorded"
)

assert_stop_handles_listener() {
  local case_name=$1 listener=$2 listener_pgid=$3 listener_parent=$4 recorded=${5:-}
  local expected_target=${6:-}
  (
    # shellcheck disable=SC1090
    source "$root_dir/scripts/dev-env.sh"
    STATE_DIR="$tmp_dir/stop-$case_name"
    mkdir -p "$STATE_DIR"
    BACKEND_PORT=18080
    FRONTEND_PORT=13000
    DESKTOP_RENDERER_PORT=5174
    local signals="$STATE_DIR/signals"
    [ -z "$recorded" ] || printf '%s\n' "$recorded" > "$(listener_pid_file web)"
    TEST_CASE=$case_name
    TEST_LISTENER=$listener
    TEST_LISTENER_PGID=$listener_pgid
    TEST_LISTENER_PARENT=$listener_parent

    component_pid() { [ "$TEST_CASE" = nested ] && printf '100'; }
    port_listener_pid() { printf '%s' "$TEST_LISTENER"; }
    process_group_id() { printf '%s' "$TEST_LISTENER_PGID"; }
    process_parent_id() {
      case "$1" in
        "$TEST_LISTENER") printf '%s' "$TEST_LISTENER_PARENT" ;;
        "$TEST_LISTENER_PARENT") printf '100' ;;
        *) printf '1' ;;
      esac
    }
    sleep() { :; }
    kill() {
      [ "$1" != -0 ] || return 1
      printf 'signal=%s target=%s\n' "$1" "${2:-}" >> "$signals"
    }

    stop_component web > "$out" 2>&1 || fail "$case_name stop failed"
    if [ -n "$expected_target" ]; then
      require_contains "$signals" "target=$expected_target"
    elif [ -s "$signals" ]; then
      fail "$case_name stop signalled an external listener: $(cat "$signals")"
    fi
  )
}

# Launcher 100 owns listener 200; listener 999 is someone else's.
assert_component_state() {
  local case_name=$1 component=$2 listener=$3 reported_commit=$4 probe_ok=$5
  local expected_state=$6 expected_detail=$7
  (
    # shellcheck disable=SC1090
    source "$root_dir/scripts/dev-env.sh"
    STATE_DIR="$tmp_dir/state-$case_name"
    mkdir -p "$STATE_DIR"
    BACKEND_PORT=18080
    FRONTEND_PORT=13000
    TEST_LISTENER=$listener
    TEST_COMMIT=$reported_commit
    TEST_PROBE_OK=$probe_ok

    component_pid() { printf '100'; }
    port_listener_pid() { printf '%s' "$TEST_LISTENER"; }
    process_group_id() { [ "$1" = 200 ] && printf '100' || printf '%s' "$1"; }
    process_parent_id() { printf '1'; }
    checkout_commit() { printf 'bbb2222'; }
    health_json() {
      printf '{"pid":%s,"commit":"%s","started_at":"2026-01-01T00:00:00Z"}' "$TEST_LISTENER" "$TEST_COMMIT"
    }
    curl() { [ "$TEST_PROBE_OK" = 1 ]; }

    local row state detail
    row="$(component_state "$component")"
    state="${row%%|*}"
    detail="${row##*|}"
    [ "$state" = "$expected_state" ] \
      || fail "$case_name state = $state, want $expected_state ($row)"
    case "$detail" in
      *"$expected_detail"*) ;;
      *) fail "$case_name detail = $detail, want it to contain: $expected_detail" ;;
    esac
  )
}

# Launcher 100 owns listener 200; listener 999 is someone else's. The probe of
# `/` fails `failed_probes` times before answering ("never" keeps failing), and
# every clock read advances 10s so the 300s wait runs out without sleeping.
assert_start_web() {
  local case_name=$1 listener=$2 failed_probes=$3 expected_status=$4
  local expected_output=$5 expected_events=$6
  (
    # shellcheck disable=SC1090
    source "$root_dir/scripts/dev-env.sh"
    STATE_DIR="$tmp_dir/start-web-$case_name"
    mkdir -p "$STATE_DIR"
    FRONTEND_PORT=13000
    ENV_FILE=.env.example
    local events="$STATE_DIR/events" probes="$STATE_DIR/probes" clock="$STATE_DIR/clock"
    printf '%s' "$listener" > "$STATE_DIR/listener"
    : > "$events"
    TEST_FAILED_PROBES=$failed_probes

    component_pid() { printf '100'; }
    port_listener_pid() { cat "$STATE_DIR/listener"; }
    process_group_id() { [ "$1" = 200 ] && printf '100' || printf '%s' "$1"; }
    process_parent_id() { printf '1'; }
    describe_port_owner() { printf 'pid %s' "$(port_listener_pid "$1")"; }
    curl() {
      local count
      count=$(($(cat "$probes" 2>/dev/null || echo 0) + 1))
      printf '%s' "$count" > "$probes"
      [ "$TEST_FAILED_PROBES" != never ] && [ "$count" -gt "$TEST_FAILED_PROBES" ]
    }
    now_epoch() {
      local now
      now=$(($(cat "$clock" 2>/dev/null || echo 1000) + 10))
      printf '%s' "$now" > "$clock"
      printf '%s' "$now"
    }
    sleep() { :; }
    kill() { printf 'kill %s\n' "$*" >> "$events"; }
    stop_component() { printf 'stop %s\n' "$1" >> "$events"; : > "$STATE_DIR/listener"; }
    launch_detached() { printf 'launch %s\n' "$1" >> "$events"; exit 0; }

    local status=0
    ( start_web ) > "$out" 2>&1 || status=$?
    [ "$status" = "$expected_status" ] \
      || { sed 's/^/  /' "$out" >&2; fail "$case_name start_web exited $status, want $expected_status"; }
    require_contains "$out" "$expected_output"
    [ "$(cat "$events")" = "$expected_events" ] \
      || fail "$case_name events = $(cat "$events"), want $expected_events"
  )
}

# ---------------------------------------------------------------------------
# An empty registry is a normal state, not an error.
# ---------------------------------------------------------------------------
dev_env list > "$out" 2>&1 || fail "list on an empty registry must succeed"
require_contains "$out" "No environments registered"

dev_env list --json > "$out" 2>&1 || fail "list --json on an empty registry must succeed"
if [ "$(cat "$out")" != "[]" ]; then
  fail "list --json on an empty registry = $(cat "$out"), want []"
fi

# ---------------------------------------------------------------------------
# Manifest serialization and user-provided names are safe. A manifest is
# sourced by Bash, so values must be shell-escaped and a name must never be
# able to walk outside envs/ before destroy eventually runs rm -rf.
# ---------------------------------------------------------------------------
quoted="$tmp_dir/quoted.env"
dangerous='a path with spaces;$(touch should-not-exist)'
bash -c 'source "$1"; write_manifest_value DIR "$2"' _ "$root_dir/scripts/dev-env.sh" "$dangerous" > "$quoted"
loaded="$(bash -c 'source "$1"; printf %s "$DIR"' _ "$quoted")"
[ "$loaded" = "$dangerous" ] || fail "manifest value did not round-trip safely"
[ ! -e "$root_dir/should-not-exist" ] || fail "loading a manifest executed its value"

status=0
dev_env up --name ../../escape > "$out" 2>&1 || status=$?
[ "$status" -ne 0 ] || fail "up accepted a path-traversing environment name"
require_contains "$out" "Invalid environment name"

status=0
dev_env up --ttl nope > "$out" 2>&1 || status=$?
[ "$status" -ne 0 ] || fail "up accepted a non-numeric TTL"
require_contains "$out" "TTL must be a positive integer"

# Rewriting an allocated database name must preserve the existing connection
# endpoint, credentials and query parameters.
rewritten="$(bash -c 'source "$1"; database_url_with_name "$2" "$3"' _ \
  "$root_dir/scripts/dev-env.sh" \
  'postgres://dev:p%40ss@127.0.0.1:55432/old_db?sslmode=require&application_name=dev' \
  'new_db')"
[ "$rewritten" = 'postgres://dev:p%40ss@127.0.0.1:55432/new_db?sslmode=require&application_name=dev' ] \
  || fail "database URL rewrite changed more than the database name: $rewritten"

# ---------------------------------------------------------------------------
# A registered environment is visible to both renderings, and the JSON one
# parses — agents read it, so a stray log line in it is a broken contract.
# ---------------------------------------------------------------------------
write_manifest "probe-901" "$tmp_dir/checkout" 901
mkdir -p "$tmp_dir/checkout"

dev_env list > "$out" 2>&1 || fail "list must succeed with one environment"
require_contains "$out" "probe-901"
require_contains "$out" "18981"

dev_env status probe-901 --json > "$out" 2>&1 || fail "status --json must succeed"
node -e '
  const fs = require("fs");
  const payload = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
  if (payload.name !== "probe-901") throw new Error("name = " + payload.name);
  if (payload.backend_port !== 18981) throw new Error("backend_port = " + payload.backend_port);
  if (payload.desktop_cdp_port !== null) throw new Error("desktop_cdp_port = " + payload.desktop_cdp_port);
  if (payload.workspace_slug !== "dev") throw new Error("workspace_slug = " + payload.workspace_slug);
  for (const key of ["api", "web", "daemon", "desktop"]) {
    if (!payload.components[key]) throw new Error("missing component " + key);
    if (payload.components[key].state !== "stopped") {
      throw new Error(key + " state = " + payload.components[key].state);
    }
  }
' "$out" || fail "status --json is not machine-readable"

# ---------------------------------------------------------------------------
# Stopping an environment that is not running is a no-op that SUCCEEDS.
#
# This is the regression that made `make down` exit 1 after reporting success:
# on bash 3.2 a command substitution whose function ends in a failing command
# aborts the whole script under `set -e`, and "no process is listening on this
# port" is that function's normal answer.
# ---------------------------------------------------------------------------
status=0
dev_env down probe-901 --components api,web > "$out" 2>&1 || status=$?
if [ "$status" -ne 0 ]; then
  echo "Observed:" >&2
  sed 's/^/  /' "$out" >&2
  fail "down on a stopped environment exited $status, want 0"
fi
require_contains "$out" "stopped"

# Commands launched through env-exec must not inherit the daemon-task identity
# hints that make human/profile CLI commands reject --profile.
write_manifest "clean-env-903" "$root_dir" 903
MULTICA_TASK_CONFIG_ROOT=/task/config \
MULTICA_TASK_WORKSPACES_ROOT=/task/workspaces \
MULTICA_WORKSPACES_ROOT=/owner/workspaces \
  dev_env exec clean-env-903 -- sh -c '
    test -z "${MULTICA_TASK_CONFIG_ROOT:-}" &&
    test -z "${MULTICA_TASK_WORKSPACES_ROOT:-}" &&
    test "$MULTICA_WORKSPACES_ROOT" = "$1"
  ' _ "$MULTICA_DEV_WORKSPACES_PARENT/multica_workspaces_dev-dev-env-test-903" \
  > "$out" 2>&1 || fail "env-exec leaked daemon task identity or owner workspaces root"

# scripts/desktop-cdp.mjs finds Desktop through env-exec: a recorded CDP port
# becomes its endpoint, and an environment without one never inherits a URL.
write_manifest "cdp-exec-906" "$root_dir" 906
printf 'DESKTOP_CDP_PORT=10129\n' >> "$MULTICA_DEV_HOME/envs/cdp-exec-906/manifest.env"
dev_env exec cdp-exec-906 -- sh -c '
  test "$MULTICA_DEV_DESKTOP_CDP_URL" = http://127.0.0.1:10129 &&
  test "$MULTICA_DEV_DESKTOP_RENDERER_URL" = http://localhost:6080
' > "$out" 2>&1 || fail "env-exec did not export the Desktop CDP and renderer URLs"
MULTICA_DEV_DESKTOP_CDP_URL=http://127.0.0.1:1 dev_env exec clean-env-903 -- sh -c '
  test -z "${MULTICA_DEV_DESKTOP_CDP_URL:-}"
' > "$out" 2>&1 || fail "env-exec leaked a CDP URL into an environment without one"

# A health response without process identity is never proof that the process is
# this checkout's freshly launched API.
if bash -c 'source "$1"; api_started_after '\''{"status":"ok"}'\'' 1' _ "$root_dir/scripts/dev-env.sh"; then
  fail "legacy /health without started_at was accepted as current"
fi

# Listener ownership follows the process tree, not only the launcher's process
# group. Turbo/pnpm can create a nested process group for Next while keeping the
# listener below the launcher in the PPID chain.
assert_listener_ownership same-pgid owned 100 200 100
assert_listener_ownership nested owned 100 420 310
assert_listener_ownership recorded owned 100 200 200 200
assert_listener_ownership external external 100 999 999
assert_nested_listener_is_recorded

# Stopping first records an owned nested listener before killing the launcher's
# process group. An unrelated port occupant never receives a signal.
assert_stop_handles_listener nested 420 310 200 "" 420
assert_stop_handles_listener external 999 999 1 888

# Status separates "ours but out of date or busy" from "not ours". Only the
# latter means inspecting the port for an external listener; a commit after
# `make up` or a slow Next compile must not read like a hijacked port.
assert_component_state api-current api 200 bbb2222 1 running "commit bbb2222"
assert_component_state api-stale api 200 aaa1111 1 stale "running aaa1111, checkout at bbb2222; run make up"
assert_component_state api-foreign api 999 bbb2222 1 mismatch "not this checkout/process"
assert_component_state api-foreign-old api 999 aaa1111 1 mismatch "not this checkout/process"
assert_component_state web-running web 200 - 1 running "pid 200"
assert_component_state web-busy web 200 - 0 busy "pid 200 is ours but not responding"
assert_component_state web-foreign web 999 - 1 mismatch "listener pid 999 is not owned"
assert_component_state web-foreign-silent web 999 - 0 mismatch "listener pid 999 is not owned"

# `make up` waits for its own web server while Next compiles instead of calling
# the port busy, restarts it only once the launch timeout runs out, and still
# refuses a listener it does not own.
assert_start_web compiling 200 3 0 "web already running on :13000 (pid 200)" ""
assert_start_web hung 200 never 0 "did not answer within 300s; restarting it" $'stop web\nlaunch web'
assert_start_web foreign 999 0 1 "Port 13000 is busy: pid 999" ""
assert_start_web foreign-silent 999 never 1 "Port 13000 is busy: pid 999" ""

# ---------------------------------------------------------------------------
# Desktop: CDP port, env file, process tree, and the daemon Desktop starts.
# ---------------------------------------------------------------------------

# A recorded CDP port is reported for Playwright, and the next allocation skips
# a port another environment already holds.
write_manifest "cdp-905" "$tmp_dir/checkout" 905
printf 'DESKTOP_CDP_PORT=19999\n' >> "$MULTICA_DEV_HOME/envs/cdp-905/manifest.env"
dev_env status cdp-905 --json > "$out" 2>&1 || fail "status --json with a CDP port must succeed"
node -e '
  const payload = JSON.parse(require("fs").readFileSync(process.argv[1], "utf8"));
  if (payload.desktop_cdp_port !== 19999) throw new Error("desktop_cdp_port = " + payload.desktop_cdp_port);
' "$out" || fail "status --json did not report the recorded CDP port"
allocated="$(bash -c 'source "$1"; port_free() { :; }; allocate_cdp_port self 19999' _ "$root_dir/scripts/dev-env.sh")"
[ "$allocated" = 20000 ] || fail "CDP allocation reused another environment's port: $allocated"
dev_env destroy cdp-905 --yes > "$out" 2>&1 || fail "destroying the CDP fixture failed"

status=0
dev_env up --components api --cdp > "$out" 2>&1 || status=$?
[ "$status" -ne 0 ] || fail "up accepted --cdp without the desktop component"
require_contains "$out" "need the desktop component"

# Clearing the port without the desktop component would leave a running
# Desktop serving it while status reports null.
status=0
dev_env up --components api --no-cdp > "$out" 2>&1 || status=$?
[ "$status" -ne 0 ] || fail "up accepted --no-cdp without the desktop component"
require_contains "$out" "need the desktop component"

status=0
dev_env up --components desktop --remote-debugging-port 80 > "$out" 2>&1 || status=$?
[ "$status" -ne 0 ] || fail "up accepted a privileged CDP port"
require_contains "$out" "between 1024 and 65535"

# `up` without the desktop component still points a missing or managed env
# file at this backend, and never rewrites a file a human wrote.
assert_desktop_env_sync() (
  local case_name=$1 initial=$2 expected=$3
  # shellcheck disable=SC1090
  source "$root_dir/scripts/dev-env.sh"
  NAME=sync-env BACKEND_PORT=18555 FRONTEND_PORT=13555
  DESKTOP_ENV_FILE="$tmp_dir/desktop-env-$case_name"
  rm -f "$DESKTOP_ENV_FILE"
  [ -z "$initial" ] || printf '%s\n' "$initial" > "$DESKTOP_ENV_FILE"
  sync_desktop_env_file > "$out" 2>&1
  grep -Fqx "$expected" "$DESKTOP_ENV_FILE" \
    || fail "$case_name env file = $(cat "$DESKTOP_ENV_FILE"), want a line: $expected"
)
assert_desktop_env_sync missing "" "VITE_API_URL=http://localhost:18555"
assert_desktop_env_sync managed-elsewhere "# Managed by scripts/dev-env.sh for environment old-env." \
  "VITE_API_URL=http://localhost:18555"
assert_desktop_env_sync managed-missing-app "# Managed by scripts/dev-env.sh for environment sync-env.
VITE_API_URL=http://localhost:18555
VITE_WS_URL=ws://localhost:18555/ws" "VITE_APP_URL=http://localhost:13555"
assert_desktop_env_sync hand-written "VITE_API_URL=https://staging.example" "VITE_API_URL=https://staging.example"

# Turbo puts Electron in a nested process group; the stop path finds it through
# the launcher's PPID chain instead.
bash -c 'sleep 30 & bash -c "sleep 30; :" & wait' 2>/dev/null &
tree_root=$!
sleep 0.5
tree="$(bash -c 'source "$1"; descendant_pids "$2"' _ "$root_dir/scripts/dev-env.sh" "$tree_root" | sort)"
for child in $tree; do kill "$child" 2>/dev/null || true; done
wait "$tree_root" 2>/dev/null || true
[ "$(printf '%s\n' "$tree" | grep -c .)" -ge 3 ] \
  || fail "descendant_pids found $(printf '%s' "$tree" | tr '\n' ' ') under $tree_root, want both sleeps and the nested shell"

# Desktop's own daemon outlives Electron. Stopping Desktop stops it, and only a
# confirmed stop lets destroy remove the profile Desktop derived from this
# backend's URL; that profile holds the pid a retried destroy needs.
desktop_profile_dir="$MULTICA_DEV_PROFILES_HOME/desktop-localhost-18080"
mkdir -p "$desktop_profile_dir"
desktop_daemon="$(sleep 60 > /dev/null 2>&1 & printf '%s' "$!")"
printf '%s\n' "$desktop_daemon" > "$desktop_profile_dir/daemon.pid"
fake_checkout="$tmp_dir/fake-checkout"
stop_mode="$tmp_dir/daemon-stop-mode"
mkdir -p "$fake_checkout/server/bin"
# The fake CLI stops the daemon only in "kill" mode. "lies" exits 0 with the
# daemon still alive, like a plain `daemon stop` that is still finishing a run.
cat > "$fake_checkout/server/bin/multica" <<EOF
#!/usr/bin/env bash
[ "\$*" = "daemon stop --force --profile desktop-localhost-18080" ] || exit 0
case "\$(cat "$stop_mode")" in
  kill)
    kill -KILL $desktop_daemon
    while kill -0 $desktop_daemon 2>/dev/null; do sleep 0.1; done
    ;;
  lies) ;;
  *) exit 1 ;;
esac
EOF
chmod +x "$fake_checkout/server/bin/multica"

printf 'lies' > "$stop_mode"
status=0
(
  # shellcheck disable=SC1090
  source "$root_dir/scripts/dev-env.sh"
  BACKEND_PORT=18080 MULTICA_BIN="$fake_checkout/server/bin/multica" DIR="$fake_checkout"
  stop_desktop_daemon
) > "$out" 2>&1 || status=$?
[ "$status" -ne 0 ] || fail "stop_desktop_daemon trusted a stop that left the daemon running"
kill -0 "$desktop_daemon" 2>/dev/null || fail "the fake daemon died before the retry test"

printf 'fails' > "$stop_mode"
write_manifest "desktop-profile-906" "$fake_checkout" 0
status=0
dev_env destroy desktop-profile-906 --yes > "$out" 2>&1 || status=$?
[ "$status" -ne 0 ] || fail "destroy succeeded while Desktop's daemon was still running"
[ -f "$desktop_profile_dir/daemon.pid" ] || fail "a failed destroy removed Desktop's daemon pid"
[ -f "$MULTICA_DEV_HOME/envs/desktop-profile-906/manifest.env" ] \
  || fail "a failed destroy discarded the manifest"
require_contains "$out" "kept Desktop's CLI profile desktop-localhost-18080"

printf 'kill' > "$stop_mode"
dev_env destroy desktop-profile-906 --yes > "$out" 2>&1 \
  || { sed 's/^/  /' "$out" >&2; fail "retrying destroy after the daemon could be stopped failed"; }
! kill -0 "$desktop_daemon" 2>/dev/null || fail "Desktop's daemon is still running after destroy"
[ ! -d "$desktop_profile_dir" ] || fail "destroy left Desktop's CLI profile behind"

# ---------------------------------------------------------------------------
# Unknown names and components fail loudly instead of doing something else.
# ---------------------------------------------------------------------------
status=0
dev_env status no-such-env > "$out" 2>&1 || status=$?
[ "$status" -ne 0 ] || fail "status on an unknown environment must fail"
require_contains "$out" "Unknown environment"

status=0
dev_env up --components nope > "$out" 2>&1 || status=$?
[ "$status" -ne 0 ] || fail "up with an unknown component must fail"
require_contains "$out" "Unknown component"

# ---------------------------------------------------------------------------
# gc reports what it would collect and touches nothing in --dry-run. An
# environment whose checkout is gone has no owner left to stop it, which is how
# 152 databases accumulated with nothing on the machine able to list them.
# ---------------------------------------------------------------------------
write_manifest "orphan-902" "$tmp_dir/deleted-checkout" 902

dev_env gc --dry-run > "$out" 2>&1 || fail "gc --dry-run must succeed"
require_contains "$out" "orphan-902 would be collected"
if grep -Fq "probe-901 would be collected" "$out"; then
  fail "gc must not collect an environment whose directory still exists"
fi
[ -f "$MULTICA_DEV_HOME/envs/orphan-902/manifest.env" ] || fail "gc --dry-run deleted a manifest"

# A failed database drop keeps the manifest and slot so cleanup can be retried;
# destroy must never print success and forget the only deletion recipe.
write_manifest "drop-fails-904" "$root_dir" 904
status=0
FAIL_DROP=1 dev_env destroy drop-fails-904 --yes > "$out" 2>&1 || status=$?
[ "$status" -ne 0 ] || fail "destroy succeeded after DROP DATABASE failed"
[ -f "$MULTICA_DEV_HOME/envs/drop-fails-904/manifest.env" ] \
  || fail "destroy discarded the manifest after DROP DATABASE failed"
require_contains "$out" "manifest and slot were kept"
dev_env destroy drop-fails-904 --yes > "$out" 2>&1 || fail "retrying destroy after database recovery failed"

# ---------------------------------------------------------------------------
# destroy consumes the manifest: the slot is free afterwards, which is what
# makes the registry an allocator rather than a second place to leak.
# ---------------------------------------------------------------------------
dev_env destroy probe-901 --yes > "$out" 2>&1 || fail "destroy must succeed"
[ ! -d "$MULTICA_DEV_HOME/envs/probe-901" ] || fail "destroy left the environment directory behind"

dev_env list > "$out" 2>&1 || fail "list must succeed after destroy"
if grep -Fq "probe-901" "$out"; then
  fail "destroyed environment is still listed"
fi

# Declining the confirmation is a successful no-op, not a failure.
printf 'n\n' | dev_env destroy orphan-902 > "$out" 2>&1 || fail "declining destroy must exit 0"
require_contains "$out" "Cancelled."
[ -d "$MULTICA_DEV_HOME/envs/orphan-902" ] || fail "declined destroy removed the environment anyway"

echo "✓ dev-env.sh registry behaviour verified"

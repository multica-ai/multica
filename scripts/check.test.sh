#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
TEST_DIR=$(mktemp -d "${TMPDIR:-/tmp}/multica-check.XXXXXX")
trap 'rm -rf "$TEST_DIR"' EXIT
mkdir -p "$TEST_DIR/scripts" "$TEST_DIR/server" "$TEST_DIR/bin"
# Keep the production script's fixed service logs inside the fixture too.
sed "s|/tmp/multica-check-|$TEST_DIR/multica-check-|g" \
  "$SCRIPT_DIR/check.sh" >"$TEST_DIR/scripts/check.sh"
export CHECK_TEST_DIR="$TEST_DIR"

cat >"$TEST_DIR/.env" <<'ENV'
PORT=18080
FRONTEND_PORT=13000
ENV

# Override process operations as well as external tools: no real service is
# started, contacted, or killed, even in the ownership/cleanup cases.
cat >"$TEST_DIR/scripts/local-env.sh" <<'FAKE'
kill() {
  if [ "$1" = "$BACKEND_PID" ]; then
    echo backend >>"$CHECK_TEST_DIR/killed"
  elif [ "$1" = "$FRONTEND_PID" ]; then
    echo frontend >>"$CHECK_TEST_DIR/killed"
  else
    echo "unexpected PID: $1" >>"$CHECK_TEST_DIR/killed"
  fi
  return "${CLEANUP_STATUS:-0}"
}
wait() { return 0; }
FAKE
cat >"$TEST_DIR/scripts/ensure-postgres.sh" <<'FAKE'
exit "${PREREQUISITE_STATUS:-0}"
FAKE
printf 'exit 0\n' >"$TEST_DIR/scripts/test-go.test.sh"
printf 'exit 0\n' >"$TEST_DIR/scripts/test-go.sh"
cat >"$TEST_DIR/bin/pnpm" <<'FAKE'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$CHECK_TEST_DIR/pnpm-calls"
if [ "$*" = "${FAIL_PHASE:-}" ]; then
  exit 23
fi
FAKE
cat >"$TEST_DIR/bin/go" <<'FAKE'
#!/usr/bin/env bash
exit 0
FAKE
cat >"$TEST_DIR/bin/curl" <<'FAKE'
#!/usr/bin/env bash
case "$*" in
  *18080*) service=backend ;;
  *13000*) service=frontend ;;
  *) exit 2 ;;
esac
# First probe sees the configured existing service; subsequent probes see the
# newly started stub as ready, so tests never sleep or access the network.
if [ ! -f "$CHECK_TEST_DIR/$service-probed" ]; then
  touch "$CHECK_TEST_DIR/$service-probed"
  case ":${START_SERVICES:-}:" in
    *":$service:"*) exit 1 ;;
  esac
fi
FAKE
chmod 755 "$TEST_DIR/bin/"*

fail() {
  echo "$label: $*" >&2
  cat "$TEST_DIR/output" >&2
  exit 1
}

# $1: label; $2: status; $3: services expected to be stopped; rest: env overrides.
run_case() {
  label=$1 expected_status=$2 expected_killed=$3
  shift 3
  rm -f "$TEST_DIR/"*-probed
  : >"$TEST_DIR/killed"
  : >"$TEST_DIR/pnpm-calls"
  set +e
  (
    cd "$TEST_DIR"
    env -u BASH_ENV ENV_FILE=.env PATH="$TEST_DIR/bin:$PATH" \
      PREREQUISITE_STATUS=0 FAIL_PHASE= START_SERVICES= CLEANUP_STATUS=0 \
      "$@" bash scripts/check.sh
  ) >"$TEST_DIR/output" 2>&1
  status=$?
  set -e
  [ "$status" -eq "$expected_status" ] || fail "status $status, want $expected_status"
  if [ "$expected_status" -eq 0 ]; then
    grep -q '^✓ All checks passed\.$' "$TEST_DIR/output" || fail "missing success footer"
    ! grep -q 'Checks FAILED' "$TEST_DIR/output" || fail "unexpected failure footer"
  else
    grep -q '^✗ Checks FAILED\.$' "$TEST_DIR/output" || fail "missing failure footer"
    ! grep -q 'All checks passed' "$TEST_DIR/output" || fail "misleading success footer"
  fi
  [ "$(cat "$TEST_DIR/killed")" = "$expected_killed" ] || fail "wrong cleanup ownership"
  echo "$label: PASS"
}

run_case 'prerequisite failure' 37 '' PREREQUISITE_STATUS=37
[ ! -s "$TEST_DIR/pnpm-calls" ] || fail 'continued after prerequisite failure'
run_case 'guarded typecheck failure' 1 '' FAIL_PHASE=typecheck
run_case 'success with existing services' 0 ''
grep -q '^exec playwright test$' "$TEST_DIR/pnpm-calls" || fail 'did not reach E2E'
run_case 'success with owned services' 0 'backend
frontend' START_SERVICES=backend:frontend
run_case 'E2E failure with owned backend' 1 backend START_SERVICES=backend FAIL_PHASE='exec playwright test'
run_case 'E2E failure with owned frontend' 1 frontend START_SERVICES=frontend FAIL_PHASE='exec playwright test'
run_case 'cleanup failure preserves failed checks' 1 'backend
frontend' START_SERVICES=backend:frontend FAIL_PHASE='exec playwright test' CLEANUP_STATUS=1

echo 'check.test.sh: PASS'

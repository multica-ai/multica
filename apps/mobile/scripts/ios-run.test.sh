#!/usr/bin/env bash
# Tests for scripts/ios-run.sh.
#
# The wrapper exists so that `expo prebuild` always runs before `expo run:ios`
# (run:ios skips prebuild whenever ios/ already exists, freezing everything
# app.config.ts owns). These tests stub pnpm on PATH and assert the call
# sequence, so they need no node_modules, no Xcode, and no device.
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
TEST_DIR=$(mktemp -d "${TMPDIR:-/tmp}/multica-ios-run.XXXXXX")
BIN_DIR="$TEST_DIR/bin"
CALLS_FILE="$TEST_DIR/pnpm-calls.log"

cleanup() {
  rm -rf "$TEST_DIR"
}
trap cleanup EXIT

mkdir -p "$BIN_DIR"
export MULTICA_TEST_PNPM_CALLS="$CALLS_FILE"

# Stub pnpm: record every invocation, and optionally fail the prebuild so the
# abort-before-run case can be exercised.
cat >"$BIN_DIR/pnpm" <<'EOF'
#!/usr/bin/env bash
set -eu

printf '%s\n' "$*" >>"$MULTICA_TEST_PNPM_CALLS"

if [ -n "${MULTICA_TEST_FAIL_PREBUILD:-}" ]; then
  case "$*" in
    *prebuild*)
      echo "stub prebuild failure" >&2
      exit 1
      ;;
  esac
fi
EOF
chmod +x "$BIN_DIR/pnpm"

PATH="$BIN_DIR:$PATH"
export PATH

fail() {
  echo "FAIL: $1" >&2
  echo "--- recorded calls ---" >&2
  cat "$CALLS_FILE" >&2 || true
  exit 1
}

# --- prebuild runs before run:ios ------------------------------------------
: >"$CALLS_FILE"
"$SCRIPT_DIR/ios-run.sh"

expected_prebuild='exec expo prebuild -p ios --no-install'
[ "$(sed -n '1p' "$CALLS_FILE")" = "$expected_prebuild" ] ||
  fail "first call should be the prebuild, got: $(sed -n '1p' "$CALLS_FILE")"
[ "$(sed -n '2p' "$CALLS_FILE")" = 'exec expo run:ios' ] ||
  fail "second call should be run:ios, got: $(sed -n '2p' "$CALLS_FILE")"
[ "$(wc -l <"$CALLS_FILE")" -eq 2 ] || fail "expected exactly 2 calls"

# --- arguments forward to run:ios only --------------------------------------
: >"$CALLS_FILE"
"$SCRIPT_DIR/ios-run.sh" --device --configuration Release

[ "$(sed -n '1p' "$CALLS_FILE")" = "$expected_prebuild" ] ||
  fail "prebuild must not receive run:ios arguments"
[ "$(sed -n '2p' "$CALLS_FILE")" = 'exec expo run:ios --device --configuration Release' ] ||
  fail "run:ios should receive the forwarded arguments"

# --- a failed prebuild aborts before run:ios --------------------------------
: >"$CALLS_FILE"
set +e
MULTICA_TEST_FAIL_PREBUILD=1 "$SCRIPT_DIR/ios-run.sh" >/dev/null 2>&1
status=$?
set -e

[ "$status" -ne 0 ] || fail "a failed prebuild should make the wrapper exit non-zero"
grep -q 'run:ios' "$CALLS_FILE" &&
  fail "run:ios must not run after a failed prebuild"

echo "ios-run.test.sh: all assertions passed"

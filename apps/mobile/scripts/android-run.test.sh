#!/usr/bin/env bash
# Tests that android-run.sh reapplies Expo config before building/installing.
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
TEST_DIR=$(mktemp -d "${TMPDIR:-/tmp}/multica-android-run.XXXXXX")
BIN_DIR="$TEST_DIR/bin"
CALLS_FILE="$TEST_DIR/pnpm-calls.log"

cleanup() {
  rm -rf "$TEST_DIR"
}
trap cleanup EXIT

mkdir -p "$BIN_DIR"
export MULTICA_TEST_PNPM_CALLS="$CALLS_FILE"

cat >"$BIN_DIR/pnpm" <<'EOF'
#!/usr/bin/env bash
set -eu
printf '%s\n' "$*" >>"$MULTICA_TEST_PNPM_CALLS"
if [ -n "${MULTICA_TEST_FAIL_PREBUILD:-}" ]; then
  case "$*" in
    *prebuild*) echo "stub prebuild failure" >&2; exit 1 ;;
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

: >"$CALLS_FILE"
"$SCRIPT_DIR/android-run.sh"
[ "$(sed -n '1p' "$CALLS_FILE")" = 'exec expo prebuild -p android --no-install' ] ||
  fail "first call should be Android prebuild"
[ "$(sed -n '2p' "$CALLS_FILE")" = 'exec expo run:android' ] ||
  fail "second call should run Android"
[ "$(wc -l <"$CALLS_FILE")" -eq 2 ] || fail "expected exactly 2 calls"

: >"$CALLS_FILE"
"$SCRIPT_DIR/android-run.sh" --device
[ "$(sed -n '2p' "$CALLS_FILE")" = 'exec expo run:android --device' ] ||
  fail "run:android should receive forwarded arguments"

: >"$CALLS_FILE"
set +e
MULTICA_TEST_FAIL_PREBUILD=1 "$SCRIPT_DIR/android-run.sh" >/dev/null 2>&1
status=$?
set -e
[ "$status" -ne 0 ] || fail "failed prebuild should return non-zero"
! grep -q 'run:android' "$CALLS_FILE" || fail "run:android must not run after prebuild fails"

echo "android-run.test.sh: all assertions passed"

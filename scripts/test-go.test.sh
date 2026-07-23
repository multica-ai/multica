#!/usr/bin/env bash
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
TEST_DIR=$(mktemp -d "${TMPDIR:-/tmp}/multica-test-go.XXXXXX")
BIN_DIR="$TEST_DIR/bin"
CALLS_FILE="$TEST_DIR/go-calls.log"
OUTPUT_FILE="$TEST_DIR/output.log"

cleanup() {
  rm -rf "$TEST_DIR"
}
trap cleanup EXIT

mkdir -p "$BIN_DIR"
export MULTICA_TEST_GO_CALLS="$CALLS_FILE"

cat >"$BIN_DIR/go" <<'EOF'
#!/usr/bin/env bash
set -eu

case "${1:-}" in
  list)
    if [ "$#" -ne 2 ] || [ "$2" != "./..." ]; then
      echo "unexpected go list arguments: $*" >&2
      exit 2
    fi
    printf '%s\n' \
      github.com/multica-ai/multica/server \
      github.com/multica-ai/multica/server/internal/daemon \
      github.com/multica-ai/multica/server/pkg/agent \
      github.com/multica-ai/multica/server/pkg/agent/internal/testutil
    ;;
  test)
    printf '%s\n' "$*" >>"$MULTICA_TEST_GO_CALLS"
    ;;
  *)
    echo "unexpected go command: $*" >&2
    exit 2
    ;;
esac
EOF
chmod 755 "$BIN_DIR/go"

PATH="$BIN_DIR:$PATH" bash "$SCRIPT_DIR/test-go.sh" --race

expected_calls='test -race github.com/multica-ai/multica/server github.com/multica-ai/multica/server/internal/daemon
test -race -p 2 -parallel 2 ./pkg/agent/...'
actual_calls=$(cat "$CALLS_FILE")
if [ "$actual_calls" != "$expected_calls" ]; then
  echo "unexpected go test calls:" >&2
  printf '%s\n' "$actual_calls" >&2
  exit 1
fi

: >"$CALLS_FILE"
set +e
PATH="$BIN_DIR:$PATH" bash "$SCRIPT_DIR/test-go.sh" --unknown >"$OUTPUT_FILE" 2>&1
status=$?
set -e

if [ "$status" -ne 2 ]; then
  echo "unknown option returned status $status, want 2" >&2
  cat "$OUTPUT_FILE" >&2
  exit 1
fi
if [ -s "$CALLS_FILE" ]; then
  echo "unknown option invoked go:" >&2
  cat "$CALLS_FILE" >&2
  exit 1
fi
if ! grep -q '^usage: .*test-go.sh \[--race\]$' "$OUTPUT_FILE"; then
  echo "unknown option did not print usage" >&2
  cat "$OUTPUT_FILE" >&2
  exit 1
fi

echo "test-go.test.sh: PASS"

#!/usr/bin/env bash
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
NAMES_FILE="$SCRIPT_DIR/agent-cli-command-names.txt"
GUARD_DIR=$(mktemp -d "${TMPDIR:-/tmp}/multica-agent-cli-guard.XXXXXX")
BIN_DIR="$GUARD_DIR/bin"
MARKER_FILE="$GUARD_DIR/invocations.log"

cleanup() {
  rm -rf "$GUARD_DIR"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

mkdir -p "$BIN_DIR"

while IFS= read -r name || [ -n "$name" ]; do
  case "$name" in
    ""|'#'*) continue ;;
    *[!A-Za-z0-9._-]*)
      echo "invalid agent CLI command name in $NAMES_FILE: $name" >&2
      exit 2
      ;;
  esac
  sentinel="$BIN_DIR/$name"
  cat >"$sentinel" <<'EOF'
#!/bin/sh
line=$(basename "$0")
if [ "$#" -gt 0 ]; then
  line="$line [arguments redacted]"
fi
printf '%s\n' "$line" >>"$MULTICA_AGENT_CLI_GUARD_MARKER"
exit 126
EOF
  chmod 755 "$sentinel"
done <"$NAMES_FILE"

if [ "${1:-}" = "--" ]; then
  shift
fi
if [ "$#" -eq 0 ]; then
  echo "usage: $0 [--] command [args...]" >&2
  exit 2
fi

set +e
PATH="$BIN_DIR:$PATH" MULTICA_AGENT_CLI_GUARD_MARKER="$MARKER_FILE" "$@"
command_status=$?
set -e

if [ -s "$MARKER_FILE" ]; then
  while IFS= read -r invocation; do
    echo "unexpected agent CLI invocation: $invocation" >&2
  done <"$MARKER_FILE"
  exit 1
fi

exit "$command_status"

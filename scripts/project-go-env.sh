#!/usr/bin/env bash
# Prefer the Go toolchain pinned for this checkout.  MULTICA_GO_BIN lets
# lightweight wrapper tests inject a fake `go` executable deterministically.
PROJECT_ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)

if [ -n "${MULTICA_GO_BIN:-}" ]; then
  GO_BIN="$MULTICA_GO_BIN"
elif [ -x "$PROJECT_ROOT/.tools/go/bin/go" ]; then
  GO_BIN="$PROJECT_ROOT/.tools/go/bin/go"
else
  GO_BIN=""
fi

if [ -n "$GO_BIN" ]; then
  if [ ! -x "$GO_BIN" ]; then
    echo "Configured Go executable is not runnable: $GO_BIN" >&2
    exit 1
  fi
  export PATH="$(dirname "$GO_BIN"):$PATH"
fi

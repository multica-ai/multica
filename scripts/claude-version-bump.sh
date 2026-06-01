#!/usr/bin/env bash
# Bump the pinned claude-code version and refresh the broker's embedded
# oauth-constants.json against the new binary.
#
# Usage:   scripts/claude-version-bump.sh [--check-only]
# Effects: mutates packaging/claude-code-version and
#          server/cmd/multica-claude-broker/oauth-constants.json
# Output:  prints the new version to stdout on a successful bump;
#          exits 0 silently (with a stderr note) if no bump is needed;
#          exits non-zero on failure.
#
# Invoked by .github/workflows/claude-version-watch.yml; also runnable
# locally to debug the bump path without a workflow round-trip.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
PIN_FILE="$ROOT/packaging/claude-code-version"
CONSTANTS_FILE="$ROOT/server/cmd/multica-claude-broker/oauth-constants.json"
EXTRACTOR_PKG="./cmd/extract-oauth-constants"

CHECK_ONLY=0
if [[ "${1:-}" == "--check-only" ]]; then CHECK_ONLY=1; fi

CURRENT="$(tr -d '[:space:]' < "$PIN_FILE")"
[ -n "$CURRENT" ] || { echo "no current version pinned in $PIN_FILE" >&2; exit 1; }

LATEST="$(npm view @anthropic-ai/claude-code version 2>/dev/null)"
[ -n "$LATEST" ] || { echo "npm view returned empty" >&2; exit 1; }

if [[ "$CURRENT" == "$LATEST" ]]; then
  echo "claude-code already at latest ($CURRENT)" >&2
  exit 0
fi
echo "claude-code: $CURRENT -> $LATEST" >&2

if [[ "$CHECK_ONLY" -eq 1 ]]; then
  exit 0
fi

# Install the new claude locally so we can run the extractor against it.
WORKDIR="$(mktemp -d)"
EXTRACTOR_BIN="$WORKDIR/extract-oauth-constants"
trap 'rm -rf "$WORKDIR"' EXIT

echo "==> installing @anthropic-ai/claude-code@$LATEST into $WORKDIR" >&2
( cd "$WORKDIR" && npm init -y >/dev/null && npm install "@anthropic-ai/claude-code@$LATEST" >/dev/null )

CLAUDE_BIN="$WORKDIR/node_modules/@anthropic-ai/claude-code/bin/claude.exe"
[ -x "$CLAUDE_BIN" ] || { echo "expected binary not found: $CLAUDE_BIN" >&2; exit 1; }

# Build the extractor from source. Fast enough (~2s) that we don't
# bother caching the binary.
echo "==> building extractor" >&2
( cd "$ROOT/server" && go build -o "$EXTRACTOR_BIN" "$EXTRACTOR_PKG" )

# Run extraction — extractor exits non-zero on any required-field miss.
echo "==> extracting constants from claude-$LATEST" >&2
"$EXTRACTOR_BIN" \
  -binary "$CLAUDE_BIN" \
  -claude-version "$LATEST" \
  -out "$CONSTANTS_FILE"

# Update the pin.
echo "$LATEST" > "$PIN_FILE"

# Print the new version to stdout for the workflow to read.
echo "$LATEST"

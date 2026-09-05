#!/usr/bin/env bash
# Fast JSON snapshot for ceo-daily-brief / cron (no HTTP server, no verify by default).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
exec python3 "$SCRIPT_DIR/ceo-workbench-server.py" --snapshot-json "$@"

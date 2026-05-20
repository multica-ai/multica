#!/usr/bin/env bash
set -euo pipefail

REPO=/Users/sara/code/firtal-cerebro

LOG_NAME=webhook
# shellcheck source=_log-rotation.sh
source "$REPO/.deploy/_log-rotation.sh"

exec /opt/homebrew/bin/webhook \
  -hooks "$REPO/.deploy/webhook-hooks.json" \
  -port 9000 \
  -ip 127.0.0.1 \
  -verbose

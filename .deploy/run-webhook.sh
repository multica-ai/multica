#!/usr/bin/env bash
set -euo pipefail

REPO=/Users/sara/code/firtal-cerebro
exec /opt/homebrew/bin/webhook \
  -hooks "$REPO/.deploy/webhook-hooks.json" \
  -port 9000 \
  -ip 127.0.0.1 \
  -verbose

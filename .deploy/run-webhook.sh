#!/usr/bin/env bash
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

LOG_NAME=webhook
# shellcheck source=_log-rotation.sh
source "$REPO/.deploy/_log-rotation.sh"

exec /opt/homebrew/bin/webhook \
  -hooks "$REPO/.deploy/webhook-hooks.json" \
  -port 9000 \
  -ip 127.0.0.1 \
  -verbose

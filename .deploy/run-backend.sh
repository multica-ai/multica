#!/usr/bin/env bash
set -euo pipefail

REPO=/Users/sara/code/firtal-cerebro
cd "$REPO"

set -a
# shellcheck source=/dev/null
source .env
set +a

exec "$REPO/server/bin/server"

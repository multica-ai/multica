#!/usr/bin/env bash
set -euo pipefail

REPO=/Users/sara/code/firtal-cerebro
cd "$REPO"

set -a
# shellcheck source=/dev/null
source .env
set +a

export PATH="/Users/sara/.nvm/versions/node/v24.13.1/bin:/opt/homebrew/bin:/usr/local/bin:$PATH"
export PORT="${FRONTEND_PORT:-4200}"

cd "$REPO/apps/web"
exec /Users/sara/.nvm/versions/node/v24.13.1/bin/pnpm start --port "$PORT"

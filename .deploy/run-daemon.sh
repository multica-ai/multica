#!/usr/bin/env bash
set -euo pipefail

REPO=/Users/sara/code/firtal-cerebro

LOG_NAME=daemon
# shellcheck source=_log-rotation.sh
source "$REPO/.deploy/_log-rotation.sh"

cd "$REPO"

set -a
# shellcheck source=/dev/null
source .env
set +a

export PATH="/Users/sara/.nvm/versions/node/v24.13.1/bin:/opt/homebrew/bin:/usr/local/bin:$PATH"

# Claude-agenter spawned af denne daemon bruger Max-kontoen jesperhvejsel@gmail.com.
export CLAUDE_CONFIG_DIR=/Users/sara/.claude-accounts/jesperhvejsel@gmail.com

# FIR-2192: the daemon no longer talks to Infisical. Per-agent secrets are
# resolved server-side via the scoped-per-user Infisical machine identity and
# shipped in the claim payload, so no Infisical credentials need to be present
# in the daemon's environment.
exec "$REPO/server/bin/multica" daemon start --profile local --foreground

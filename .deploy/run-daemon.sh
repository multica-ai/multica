#!/usr/bin/env bash
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

LOG_NAME=daemon
# shellcheck source=_log-rotation.sh
source "$REPO/.deploy/_log-rotation.sh"

cd "$REPO"

set -a
# shellcheck source=/dev/null
source .env
set +a

export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"

# Claude-agenter spawned af denne daemon bruger Max-kontoen jesperhvejsel@gmail.com.
# Per-host overridable; defaults under the running user's home rather than a
# hardcoded /Users/sara path (FIR-2049).
export CLAUDE_CONFIG_DIR="${CLAUDE_CONFIG_DIR:-$HOME/.claude-accounts/jesperhvejsel@gmail.com}"

# FIR-2192: the daemon no longer talks to Infisical. Per-agent secrets are
# resolved server-side via the scoped-per-user Infisical machine identity and
# shipped in the claim payload, so no Infisical credentials need to be present
# in the daemon's environment.
exec "$REPO/server/bin/multica" daemon start --profile local --foreground

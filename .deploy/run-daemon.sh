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

# Include ~/.local/bin so the daemon can resolve agent runtimes installed
# under the user's home (e.g. `claude`, installed by the official installer
# under $HOME/.local/bin). Without this, launchd's minimal PATH causes the
# daemon to fail bootstrap with "no agent runtime found" — daemon now hard-
# fails if no runtime resolves at start, rather than silently degrading.
export PATH="$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:$PATH"

# Claude-agenter bruger default config dir ($HOME/.claude) saa de arver
# den interaktive Claude Code session's login paa hosten.
#
# Hvis CLAUDE_CONFIG_DIR saettes eksplicit — selv til samme vaerdi som default
# — bruger Claude Code en sti-hashet keychain entry ("Claude Code-credentials-
# <sha8>") i stedet for den unsuffixede entry som /login skriver til naar
# CLAUDE_CONFIG_DIR er unset. Det betyder: en eksplicit export pegende paa
# en bestemt brugers account-mappe under .claude-accounts/ binder daemonen
# haardt til den brugers login og lader spawnede agents fejle med "Not
# logged in" paa hosts hvor den keychain entry ikke findes.
#
# Per-host override: sat den i $REPO/.env hvis daemonen skal koere mod et
# specifikt account. Per-agent override via Multica CustomEnv.
unset CLAUDE_CONFIG_DIR

# FIR-2192: the daemon no longer talks to Infisical. Per-agent secrets are
# resolved server-side via the scoped-per-user Infisical machine identity and
# shipped in the claim payload, so no Infisical credentials need to be present
# in the daemon's environment.
exec "$REPO/server/bin/multica" daemon start --profile local --foreground

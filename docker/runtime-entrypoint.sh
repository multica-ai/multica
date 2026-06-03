#!/usr/bin/env bash
# Entrypoint for the containerized Multica cloud runtime (see CLOUD_RUNTIME.md).
#
# Provisions ~/.multica/profiles/local/config.json (unless a persisted volume
# already has one), optionally wires git auth for private repo checkouts, then
# runs the daemon in the foreground so the container's process manager owns it.
#
# Config is provisioned from ONE of these, in order of preference:
#   1. A persisted config.json on the mounted volume (survives restarts).
#   2. A long-lived daemon token: MULTICA_DAEMON_TOKEN + MULTICA_SERVER_URL
#      + MULTICA_WORKSPACE_ID  (recommended for a server — robust across restarts).
#   3. A one-time setup token: MULTICA_SETUP_TOKEN + MULTICA_SERVER_URL
#      (exchanged once for a daemon token; the setup token is single-use, 30 min TTL).
set -euo pipefail

PROFILE="local"
CONFIG_DIR="$HOME/.multica"
PROFILE_DIR="$CONFIG_DIR/profiles/$PROFILE"
CONFIG_FILE="$PROFILE_DIR/config.json"
mkdir -p "$PROFILE_DIR"

log() { printf '%s runtime: %s\n' "$(date -Iseconds)" "$1" >&2; }

write_config() {
  # $1=server_url $2=app_url $3=token $4=workspace_id
  cat > "$CONFIG_FILE" <<JSON
{
  "server_url": "$1",
  "app_url": "$2",
  "token": "$3",
  "workspace_id": "$4"
}
JSON
  chmod 600 "$CONFIG_FILE"
  # Mirror to the root path so plain \`multica\` calls (no --profile) share auth.
  cp "$CONFIG_FILE" "$CONFIG_DIR/config.json"
  chmod 600 "$CONFIG_DIR/config.json"
}

if [[ -s "$CONFIG_FILE" ]]; then
  log "using persisted config at $CONFIG_FILE"
elif [[ -n "${MULTICA_DAEMON_TOKEN:-}" && -n "${MULTICA_SERVER_URL:-}" && -n "${MULTICA_WORKSPACE_ID:-}" ]]; then
  log "writing config from MULTICA_DAEMON_TOKEN"
  write_config "${MULTICA_SERVER_URL%/}" "${MULTICA_APP_URL:-${MULTICA_SERVER_URL%/}}" "$MULTICA_DAEMON_TOKEN" "$MULTICA_WORKSPACE_ID"
elif [[ -n "${MULTICA_SETUP_TOKEN:-}" && -n "${MULTICA_SERVER_URL:-}" ]]; then
  log "exchanging MULTICA_SETUP_TOKEN for a daemon token"
  RESP=$(curl -fsSL -X POST "${MULTICA_SERVER_URL%/}/api/runtime-setup/exchange" \
    -H "Content-Type: application/json" \
    -d "{\"token\":\"$MULTICA_SETUP_TOKEN\",\"device_label\":\"${MULTICA_DEVICE_LABEL:-cloud-runtime}\"}")
  DAEMON_TOKEN=$(printf '%s' "$RESP" | jq -r '.daemon_token // empty')
  WS_ID=$(printf '%s' "$RESP" | jq -r '.workspace_id // empty')
  SERVER_URL=$(printf '%s' "$RESP" | jq -r '.server_url // empty')
  APP_URL=$(printf '%s' "$RESP" | jq -r '.app_url // empty')
  if [[ -z "$DAEMON_TOKEN" || -z "$WS_ID" ]]; then
    log "FATAL: setup-token exchange failed: $RESP"; exit 1
  fi
  write_config "${SERVER_URL:-${MULTICA_SERVER_URL%/}}" "${APP_URL:-${MULTICA_SERVER_URL%/}}" "$DAEMON_TOKEN" "$WS_ID"
  log "linked to workspace $WS_ID"
else
  log "FATAL: no credentials. Provide a persisted config volume, or"
  log "  MULTICA_DAEMON_TOKEN + MULTICA_SERVER_URL + MULTICA_WORKSPACE_ID, or"
  log "  MULTICA_SETUP_TOKEN + MULTICA_SERVER_URL."
  exit 1
fi

# Git auth for cloning private Firtal repos into task worktrees.
if [[ -n "${GITHUB_TOKEN:-}" ]]; then
  git config --global url."https://x-access-token:${GITHUB_TOKEN}@github.com/".insteadOf "https://github.com/"
  git config --global credential.helper store
fi
# A committer identity is required for any agent commit.
git config --global user.name "${GIT_AUTHOR_NAME:-Multica Cloud Runtime}"
git config --global user.email "${GIT_AUTHOR_EMAIL:-runtime@firtal.com}"

# Claude auth mode — choose per runtime via CLAUDE_AUTH_MODE:
#   api_key : use ANTHROPIC_API_KEY (clean, no manual login). Opts the daemon out
#             of the OAuth-only default so the key is passed to Claude Code.
#   max     : use a Claude Max / OAuth login persisted on the .multica/.claude volume
#             (run `claude` once interactively in the container to log in).
# Default: api_key when a key is present, otherwise max.
#
# The value is normalized leniently: case-insensitive, and common synonyms
# (e.g. "ANTHROPIC_API_KEY", "key", "oauth", "login") map to the right mode. An
# unrecognized value does NOT crash the container — it warns and falls back to
# auto-detect, so a typo in the dashboard can never take the runtime down.
auto_auth_mode() { [[ -n "${ANTHROPIC_API_KEY:-}" ]] && echo api_key || echo max; }
RAW_AUTH_MODE="${CLAUDE_AUTH_MODE:-$(auto_auth_mode)}"
case "$(printf '%s' "$RAW_AUTH_MODE" | tr '[:upper:]' '[:lower:]')" in
  api_key|apikey|api-key|key|anthropic|anthropic_api_key|anthropic-api-key)
    CLAUDE_AUTH_MODE=api_key ;;
  max|oauth|login|claude|claude_max|claude-max)
    CLAUDE_AUTH_MODE=max ;;
  *)
    CLAUDE_AUTH_MODE="$(auto_auth_mode)"
    log "WARN: unrecognized CLAUDE_AUTH_MODE='$RAW_AUTH_MODE' (expected api_key or max) — falling back to '$CLAUDE_AUTH_MODE'." ;;
esac

case "$CLAUDE_AUTH_MODE" in
  api_key)
    if [[ -z "${ANTHROPIC_API_KEY:-}" ]]; then
      log "FATAL: CLAUDE_AUTH_MODE=api_key but ANTHROPIC_API_KEY is unset."; exit 1
    fi
    export MULTICA_RUNTIME_ALLOW_API_KEY=1
    log "Claude auth: API key (ANTHROPIC_API_KEY)"
    ;;
  max)
    # OAuth-only default: ensure no stray key shadows the persisted login.
    unset ANTHROPIC_API_KEY ANTHROPIC_AUTH_TOKEN MULTICA_RUNTIME_ALLOW_API_KEY || true
    if [[ ! -s "$HOME/.claude/.credentials.json" && ! -d "$HOME/.claude-accounts" ]]; then
      log "WARN: CLAUDE_AUTH_MODE=max but no persisted Claude login found — run 'claude' once in the container to /login, and persist $HOME/.claude."
    fi
    log "Claude auth: Max/OAuth login"
    ;;
esac

log "starting daemon (profile=$PROFILE, foreground)"
exec multica daemon start --profile "$PROFILE" --foreground

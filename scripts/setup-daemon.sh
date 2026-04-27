#!/usr/bin/env bash
# Setup daemon configuration for the current checkout
# Usage: scripts/setup-daemon.sh [ENV_FILE] [profile-name]
#
# This script:
# 1. Verifies the backend is running
# 2. Creates a test user and PAT via automated auth
# 3. Creates a workspace
# 4. Generates CLI config with unique profile name
# 5. Outputs the profile name for use in subsequent commands

set -euo pipefail

ENV_FILE="${1:-.env}"
CUSTOM_PROFILE="${2:-}"

ensure_env_file() {
  if [ -f "$ENV_FILE" ]; then
    return
  fi

  if [ "$ENV_FILE" = ".env.worktree" ] || [ -f .git ]; then
    echo "==> Worktree detected. Generating .env.worktree..."
    bash scripts/init-worktree-env.sh .env.worktree
    ENV_FILE=".env.worktree"
    return
  fi

  if [ "$ENV_FILE" = ".env" ] && [ -f .env.example ]; then
    echo "==> Creating .env from .env.example..."
    cp .env.example .env
    return
  fi

  echo "Error: env file not found: $ENV_FILE" >&2
  exit 1
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Error: required command not found: $1" >&2
    exit 1
  fi
}

config_token_for_profile() {
  local profile_name="$1"
  local path="$HOME/.multica/profiles/$profile_name/config.json"
  if [ -f "$path" ]; then
    jq -r '.token // empty' "$path"
    return
  fi
  printf ''
}

default_cli_token() {
  local path="$HOME/.multica/config.json"
  if [ -f "$path" ]; then
    jq -r '.token // empty' "$path"
    return
  fi
  printf ''
}

workspaces_with_token() {
  local token="$1"
  curl -s -X GET "$SERVER/api/workspaces" \
    -H "Authorization: Bearer $token"
}

token_is_valid() {
  local token="$1"
  local response
  response="$(workspaces_with_token "$token")"
  if echo "$response" | jq -e 'type == "array"' >/dev/null 2>&1; then
    return 0
  fi
  return 1
}

workspace_id_by_slug() {
  local token="$1"
  local slug="$2"
  workspaces_with_token "$token" | jq -r --arg slug "$slug" 'map(select(.slug == $slug))[0].id // empty'
}

first_workspace_id() {
  local token="$1"
  workspaces_with_token "$token" | jq -r '.[0].id // empty'
}

require_cmd curl
require_cmd jq
ensure_env_file

# Load env variables robustly (supports quoted values and spaces)
set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

PORT="${PORT:-8080}"
FRONTEND_PORT="${FRONTEND_PORT:-3000}"
POSTGRES_DB="${POSTGRES_DB:-multica}"
SERVER="http://localhost:${PORT}"
APP_URL="http://localhost:${FRONTEND_PORT}"

# Compute profile name if not provided
if [ -z "$CUSTOM_PROFILE" ]; then
  WORKTREE_DIR="$(basename "$PWD")"
  SLUG="$(printf '%s' "$WORKTREE_DIR" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/_/g; s/__*/_/g; s/^_//; s/_$//')"
  HASH="$(printf '%s' "$PWD" | cksum | awk '{print $1}')"
  OFFSET=$((HASH % 1000))
  PROFILE="dev-${SLUG}-${OFFSET}"
else
  PROFILE="$CUSTOM_PROFILE"
fi

echo "==> Daemon setup for profile: $PROFILE"
echo "==> Server: $SERVER"
echo "==> App URL: $APP_URL"
echo "==> Database: $POSTGRES_DB"
echo ""

# Step 1: Verify backend is running
echo "==> Checking backend health..."
MAX_RETRIES=30
RETRY_COUNT=0
while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
  if curl -sf "$SERVER/health" > /dev/null 2>&1; then
    echo "✓ Backend is running"
    break
  fi
  RETRY_COUNT=$((RETRY_COUNT + 1))
  if [ $RETRY_COUNT -eq $MAX_RETRIES ]; then
    echo "Error: Backend at $SERVER is not responding after $MAX_RETRIES attempts" >&2
    echo "Hint: Run 'make start' or 'make dev' first" >&2
    exit 1
  fi
  sleep 1
done

# Step 2: Create test user and get JWT token
AUTH_TOKEN=""

# First preference: existing profile token (fully idempotent re-run).
EXISTING_PROFILE_TOKEN="$(config_token_for_profile "$PROFILE")"
if [ -n "$EXISTING_PROFILE_TOKEN" ] && token_is_valid "$EXISTING_PROFILE_TOKEN"; then
  AUTH_TOKEN="$EXISTING_PROFILE_TOKEN"
  echo "==> Reusing existing profile token"
fi

# Second preference: automated dev auth flow (works when APP_ENV != production).
if [ -z "$AUTH_TOKEN" ]; then
  echo "==> Creating test user (dev@localhost)..."
  SEND_CODE_RESPONSE=$(curl -s -X POST "$SERVER/auth/send-code" \
    -H "Content-Type: application/json" \
    -d '{"email": "dev@localhost"}')

  if echo "$SEND_CODE_RESPONSE" | grep -q '"error"'; then
    echo "  (User may already exist)"
  else
    echo "  Code sent to dev@localhost"
  fi

  echo "==> Verifying code (888888)..."
  JWT=$(curl -s -X POST "$SERVER/auth/verify-code" \
    -H "Content-Type: application/json" \
    -d '{"email": "dev@localhost", "code": "888888"}' | jq -r '.token // empty')

  if [ -n "$JWT" ]; then
    echo "✓ JWT obtained"
    echo "==> Creating Personal Access Token..."
    PAT=$(curl -s -X POST "$SERVER/api/tokens" \
      -H "Authorization: Bearer $JWT" \
      -H "Content-Type: application/json" \
      -d '{"name": "auto-dev", "expires_in_days": 365}' | jq -r '.token // empty')
    if [ -n "$PAT" ]; then
      AUTH_TOKEN="$PAT"
      echo "✓ PAT created"
    fi
  fi
fi

# Third preference: token from default CLI config (from `multica login`).
if [ -z "$AUTH_TOKEN" ]; then
  DEFAULT_TOKEN="$(default_cli_token)"
  if [ -n "$DEFAULT_TOKEN" ] && token_is_valid "$DEFAULT_TOKEN"; then
    AUTH_TOKEN="$DEFAULT_TOKEN"
    echo "==> Reusing token from ~/.multica/config.json"
  fi
fi

if [ -z "$AUTH_TOKEN" ]; then
  echo "Error: Could not obtain a valid auth token for daemon setup." >&2
  echo "Hints:" >&2
  echo "  - If local dev server uses APP_ENV=production, code 888888 is disabled." >&2
  echo "  - Run 'multica login' once to seed ~/.multica/config.json, then rerun make daemon." >&2
  exit 1
fi

# Step 4: Create workspace
echo "==> Creating workspace 'Dev'..."
WS=$(curl -s -X POST "$SERVER/api/workspaces" \
  -H "Authorization: Bearer $AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "Dev", "slug": "dev"}' | jq -r '.id // empty')

if [ -z "$WS" ]; then
  # Workspace may already exist, fetch by slug first.
  echo "  (Workspace may already exist, fetching...)"
  WS=$(workspace_id_by_slug "$AUTH_TOKEN" "dev")
  if [ -z "$WS" ]; then
    WS=$(first_workspace_id "$AUTH_TOKEN")
  fi
fi

if [ -z "$WS" ]; then
  echo "Error: Failed to create or get workspace" >&2
  exit 1
fi

echo "✓ Workspace ID: $WS"

# Step 5: Write CLI config
CONFIG_DIR="$HOME/.multica/profiles/$PROFILE"
mkdir -p "$CONFIG_DIR"

CONFIG_FILE="$CONFIG_DIR/config.json"
cat > "$CONFIG_FILE" << EOF
{
  "server_url": "$SERVER",
  "app_url": "$APP_URL",
  "token": "$AUTH_TOKEN",
  "workspace_id": "$WS",
  "watched_workspaces": [{"id": "$WS", "name": "Dev"}]
}
EOF

echo "✓ Config written to: $CONFIG_FILE"
echo ""
echo "==> Daemon setup complete!"
echo "==> Profile name: $PROFILE"
echo ""

# Output profile name for script consumption
echo "$PROFILE"

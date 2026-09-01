#!/usr/bin/env bash
# Bring up a throwaway Mattermost server and provision everything the
# Mattermost adapter's end-to-end suite needs, then print the environment block
# that suite reads.
#
#   ./scripts/mattermost-e2e-up.sh            # start + provision, print env
#   eval "$(./scripts/mattermost-e2e-up.sh)"  # ...and export it
#   ./scripts/mattermost-e2e-down.sh          # tear the container down
#
# Everything it creates lives inside one container, so tearing that down is a
# complete cleanup: no state is written outside Docker.
#
# The suite it feeds is `server/internal/integrations/mattermost` behind the
# `mattermoste2e` build tag — see that package's e2e_test.go for how to run it.
set -euo pipefail

CONTAINER="${MM_E2E_CONTAINER:-mm-e2e}"
IMAGE="${MM_E2E_IMAGE:-mattermost/mattermost-preview:latest}"
PORT="${MM_E2E_PORT:-8065}"
BASE="http://localhost:${PORT}"

ADMIN_EMAIL="admin@example.com"
ADMIN_USER="e2eadmin"
ADMIN_PASS="E2eAdminPassw0rd!"
HUMAN_EMAIL="human@example.com"
HUMAN_USER="e2ehuman"
HUMAN_PASS="E2eHumanPassw0rd!"
TEAM_NAME="e2e-team"
CHANNEL_NAME="e2e-channel"
BOT_USERNAME="multica-e2e-bot"

log() { printf '%s\n' "$*" >&2; }

# api METHOD PATH [BODY] [TOKEN] — returns the response body on stdout.
api() {
  local method="$1" path="$2" body="${3:-}" token="${4:-}"
  local args=(-sS -X "$method" "${BASE}/api/v4${path}" -H 'Content-Type: application/json')
  [[ -n "$token" ]] && args+=(-H "Authorization: Bearer ${token}")
  [[ -n "$body" ]] && args+=(-d "$body")
  curl "${args[@]}"
}

json() { node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>{try{const j=JSON.parse(s);const v=process.argv[1].split(".").reduce((a,k)=>a==null?a:a[k],j);process.stdout.write(v==null?"":String(v))}catch(e){process.stdout.write("")}})' "$1"; }

# ---- 1. container -----------------------------------------------------------

if [[ "$(docker inspect -f '{{.State.Running}}' "$CONTAINER" 2>/dev/null || echo false)" != "true" ]]; then
  log "starting ${CONTAINER} from ${IMAGE}"
  docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
  docker run -d --name "$CONTAINER" -p "${PORT}:8065" "$IMAGE" >/dev/null
else
  log "reusing running container ${CONTAINER}"
fi

log "waiting for ${BASE} to answer /system/ping (first boot takes several minutes)"
deadline=$(( $(date +%s) + 600 ))
until curl -sf "${BASE}/api/v4/system/ping" >/dev/null 2>&1; do
  if (( $(date +%s) > deadline )); then
    log "ERROR: Mattermost did not become ready within 10 minutes"
    exit 1
  fi
  sleep 5
done
log "server is up"

# ---- 2. admin ---------------------------------------------------------------

# The very first user created on a fresh install becomes system admin. On a
# reused container this 400s harmlessly and the login below still works.
api POST /users "$(printf '{"email":"%s","username":"%s","password":"%s"}' "$ADMIN_EMAIL" "$ADMIN_USER" "$ADMIN_PASS")" >/dev/null || true

ADMIN_TOKEN="$(curl -sS -i -X POST "${BASE}/api/v4/users/login" \
  -H 'Content-Type: application/json' \
  -d "$(printf '{"login_id":"%s","password":"%s"}' "$ADMIN_EMAIL" "$ADMIN_PASS")" \
  | tr -d '\r' | awk 'tolower($1)=="token:"{print $2}')"

if [[ -z "$ADMIN_TOKEN" ]]; then
  log "ERROR: could not log in as ${ADMIN_EMAIL}"
  exit 1
fi
ADMIN_ID="$(api GET /users/me '' "$ADMIN_TOKEN" | json id)"
log "admin ${ADMIN_USER} (${ADMIN_ID})"

# Bot accounts and personal access tokens are both off by default in the
# preview image; the suite needs both.
api PUT /config "$(api GET /config '' "$ADMIN_TOKEN" | node -e '
let s="";process.stdin.on("data",d=>s+=d).on("end",()=>{
  const c=JSON.parse(s);
  c.ServiceSettings.EnableBotAccountCreation=true;
  c.ServiceSettings.EnableUserAccessTokens=true;
  process.stdout.write(JSON.stringify(c));
})')" "$ADMIN_TOKEN" >/dev/null
log "enabled bot accounts + personal access tokens"

# ---- 3. team, human user, channel ------------------------------------------

TEAM_ID="$(api POST /teams "$(printf '{"name":"%s","display_name":"E2E Team","type":"O"}' "$TEAM_NAME")" "$ADMIN_TOKEN" | json id)"
[[ -z "$TEAM_ID" ]] && TEAM_ID="$(api GET "/teams/name/${TEAM_NAME}" '' "$ADMIN_TOKEN" | json id)"
log "team ${TEAM_NAME} (${TEAM_ID})"

HUMAN_ID="$(api POST /users "$(printf '{"email":"%s","username":"%s","password":"%s"}' "$HUMAN_EMAIL" "$HUMAN_USER" "$HUMAN_PASS")" "$ADMIN_TOKEN" | json id)"
[[ -z "$HUMAN_ID" ]] && HUMAN_ID="$(api GET "/users/username/${HUMAN_USER}" '' "$ADMIN_TOKEN" | json id)"
api POST "/teams/${TEAM_ID}/members" "$(printf '{"team_id":"%s","user_id":"%s"}' "$TEAM_ID" "$HUMAN_ID")" "$ADMIN_TOKEN" >/dev/null || true
log "human ${HUMAN_USER} (${HUMAN_ID})"

HUMAN_TOKEN="$(curl -sS -i -X POST "${BASE}/api/v4/users/login" \
  -H 'Content-Type: application/json' \
  -d "$(printf '{"login_id":"%s","password":"%s"}' "$HUMAN_EMAIL" "$HUMAN_PASS")" \
  | tr -d '\r' | awk 'tolower($1)=="token:"{print $2}')"

CHANNEL_ID="$(api POST /channels "$(printf '{"team_id":"%s","name":"%s","display_name":"E2E Channel","type":"O"}' "$TEAM_ID" "$CHANNEL_NAME")" "$ADMIN_TOKEN" | json id)"
[[ -z "$CHANNEL_ID" ]] && CHANNEL_ID="$(api GET "/teams/${TEAM_ID}/channels/name/${CHANNEL_NAME}" '' "$ADMIN_TOKEN" | json id)"
api POST "/channels/${CHANNEL_ID}/members" "$(printf '{"user_id":"%s"}' "$HUMAN_ID")" "$ADMIN_TOKEN" >/dev/null || true
log "channel ${CHANNEL_NAME} (${CHANNEL_ID})"

# ---- 4. bot account + token -------------------------------------------------

BOT_USER_ID="$(api POST /bots "$(printf '{"username":"%s","display_name":"Multica E2E Bot"}' "$BOT_USERNAME")" "$ADMIN_TOKEN" | json user_id)"
[[ -z "$BOT_USER_ID" ]] && BOT_USER_ID="$(api GET "/users/username/${BOT_USERNAME}" '' "$ADMIN_TOKEN" | json id)"
api POST "/teams/${TEAM_ID}/members" "$(printf '{"team_id":"%s","user_id":"%s"}' "$TEAM_ID" "$BOT_USER_ID")" "$ADMIN_TOKEN" >/dev/null || true
api POST "/channels/${CHANNEL_ID}/members" "$(printf '{"user_id":"%s"}' "$BOT_USER_ID")" "$ADMIN_TOKEN" >/dev/null || true

BOT_TOKEN="$(api POST "/users/${BOT_USER_ID}/tokens" '{"description":"multica e2e"}' "$ADMIN_TOKEN" | json token)"
if [[ -z "$BOT_TOKEN" ]]; then
  log "ERROR: could not issue a bot access token"
  exit 1
fi
log "bot ${BOT_USERNAME} (${BOT_USER_ID})"

# A direct-message channel between the bot and the human, so the suite can
# exercise the p2p path without a mention.
DM_CHANNEL_ID="$(api POST /channels/direct "$(printf '["%s","%s"]' "$BOT_USER_ID" "$HUMAN_ID")" "$ADMIN_TOKEN" | json id)"
log "dm channel (${DM_CHANNEL_ID})"

# ---- 5. emit ----------------------------------------------------------------

cat <<ENVEOF
export MULTICA_MM_E2E=1
export MULTICA_MM_E2E_URL='${BASE}'
export MULTICA_MM_E2E_BOT_TOKEN='${BOT_TOKEN}'
export MULTICA_MM_E2E_BOT_USER_ID='${BOT_USER_ID}'
export MULTICA_MM_E2E_BOT_USERNAME='${BOT_USERNAME}'
export MULTICA_MM_E2E_HUMAN_TOKEN='${HUMAN_TOKEN}'
export MULTICA_MM_E2E_HUMAN_USER_ID='${HUMAN_ID}'
export MULTICA_MM_E2E_CHANNEL_ID='${CHANNEL_ID}'
export MULTICA_MM_E2E_DM_CHANNEL_ID='${DM_CHANNEL_ID}'
export MULTICA_MM_E2E_ADMIN_TOKEN='${ADMIN_TOKEN}'
ENVEOF

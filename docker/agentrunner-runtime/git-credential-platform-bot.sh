#!/usr/bin/env bash
#
# git-credential-platform-bot — git credential helper that mints a short-lived
# GitHub App installation token for the "Platform Bot" app and hands it to git.
#
# git invokes this helper on every github.com operation (clone/fetch/push). On
# "get" we mint (or reuse a cached) installation access token and emit it as the
# password with username x-access-token. Other operations (store/erase) are
# no-ops — the token is server-minted and short-lived, nothing to persist.
#
# Required env (injected into the runner pod):
#   GITHUB_APP_ID                 numeric app id
#   GITHUB_APP_INSTALLATION_ID    numeric installation id (g2crowd org install)
#   GITHUB_APP_PRIVATE_KEY        the app's RSA private key, PEM format
#
# When any of these are absent the helper exits 0 emitting nothing, so git falls
# through to its other helpers and the pod still boots without GitHub auth.

set -euo pipefail

[ "${1:-}" = "get" ] || exit 0

if [ -z "${GITHUB_APP_ID:-}" ] || [ -z "${GITHUB_APP_INSTALLATION_ID:-}" ] || [ -z "${GITHUB_APP_PRIVATE_KEY:-}" ]; then
  exit 0
fi

API="${GITHUB_API_URL:-https://api.github.com}"
CACHE_DIR="${HOME}/.cache/agentrunner"
CACHE_FILE="${CACHE_DIR}/gh-app-token"
# Re-mint when fewer than this many seconds remain on the cached token.
REFRESH_SKEW=300

b64url() { openssl base64 -A | tr '+/' '-_' | tr -d '='; }

emit_token() {
  printf 'username=x-access-token\npassword=%s\n' "$1"
}

# Reuse a cached token while it has comfortable life left. Cache format:
#   line 1: absolute expiry, unix epoch seconds
#   line 2: the installation token
if [ -f "${CACHE_FILE}" ]; then
  exp=$(sed -n '1p' "${CACHE_FILE}" 2>/dev/null || echo 0)
  tok=$(sed -n '2p' "${CACHE_FILE}" 2>/dev/null || echo "")
  if [ -n "${tok}" ] && [ "${exp}" -gt "$(( $(date +%s) + REFRESH_SKEW ))" ] 2>/dev/null; then
    emit_token "${tok}"
    exit 0
  fi
fi

# ── Mint a fresh installation token ──────────────────────────────────────────
now=$(date +%s)
# iat backdated 60s to tolerate clock skew; exp capped well under GitHub's 10m.
header='{"alg":"RS256","typ":"JWT"}'
payload=$(printf '{"iat":%d,"exp":%d,"iss":"%s"}' "$((now - 60))" "$((now + 540))" "${GITHUB_APP_ID}")

signing_input="$(printf '%s' "${header}" | b64url).$(printf '%s' "${payload}" | b64url)"
signature=$(printf '%s' "${signing_input}" \
  | openssl dgst -sha256 -sign <(printf '%s' "${GITHUB_APP_PRIVATE_KEY}") \
  | b64url)
jwt="${signing_input}.${signature}"

response=$(curl -fsS -X POST \
  -H "Authorization: Bearer ${jwt}" \
  -H "Accept: application/vnd.github+json" \
  -H "X-GitHub-Api-Version: 2022-11-28" \
  "${API}/app/installations/${GITHUB_APP_INSTALLATION_ID}/access_tokens")

token=$(printf '%s' "${response}" | jq -r '.token // empty')
expires_at=$(printf '%s' "${response}" | jq -r '.expires_at // empty')

if [ -z "${token}" ]; then
  echo "git-credential-platform-bot: failed to mint installation token" >&2
  exit 1
fi

# Cache for reuse by subsequent git/gh calls within the token's lifetime.
exp_epoch=$(date -d "${expires_at}" +%s 2>/dev/null || echo "$((now + 3600))")
mkdir -p "${CACHE_DIR}"
umask 077
printf '%s\n%s\n' "${exp_epoch}" "${token}" > "${CACHE_FILE}"

emit_token "${token}"

#!/usr/bin/env bash
#
# git-credential-platform-bot — git credential helper that mints short-lived
# GitHub App installation tokens and hands them to git.
#
# Supports cross-org access via dynamic installation discovery: instead of a
# hardcoded per-org installation ID, the helper calls
# GET /repos/{owner}/{repo}/installation (or GET /orgs/{owner}/installation)
# using only the App JWT, which resolves the correct installation for any org
# where the Enterprise App is installed.
#
# Required env:
#   GITHUB_APP_ID          numeric app id
#   GITHUB_APP_PRIVATE_KEY the app's RSA private key, PEM format
#
# Optional env:
#   GITHUB_DEFAULT_ORG  fallback org when git doesn't send a path= line
#                       (used by the gh wrapper and non-path-aware callers)
#   GITHUB_API_URL      override for GitHub API base (default: https://api.github.com)
#
# When required env is absent the helper exits 0 emitting nothing, so git falls
# through to its other helpers and the pod still boots without GitHub auth.

set -euo pipefail

[ "${1:-}" = "get" ] || exit 0

if [ -z "${GITHUB_APP_ID:-}" ] || [ -z "${GITHUB_APP_PRIVATE_KEY:-}" ]; then
  exit 0
fi

API="${GITHUB_API_URL:-https://api.github.com}"
CACHE_DIR="${HOME}/.cache/agentrunner"
# Re-mint when fewer than this many seconds remain on the cached token.
REFRESH_SKEW=300

b64url() { openssl base64 -A | tr '+/' '-_' | tr -d '='; }

emit_token() {
  printf 'username=x-access-token\npassword=%s\n' "$1"
}

# Read stdin: git sends key=value lines terminated by a blank line.
# With credential.useHttpPath=true, git includes path=owner/repo.git.
owner="" repo=""
while IFS= read -r line && [ -n "$line" ]; do
  case "$line" in
    path=*) val="${line#path=}"; owner="${val%%/*}"; repo="${val#*/}"; repo="${repo%.git}" ;;
  esac
done

# Fall back to GITHUB_DEFAULT_ORG when git didn't send a path.
if [ -z "$owner" ]; then
  owner="${GITHUB_DEFAULT_ORG:-}"
fi

if [ -z "$owner" ]; then
  # No org context — fall through so git tries other helpers.
  exit 0
fi

# Per-org cache: tokens are org-scoped so one cached token covers all repos
# in an org without needing per-repo minting.
CACHE_FILE="${CACHE_DIR}/gh-app-token-${owner}"

if [ -f "${CACHE_FILE}" ]; then
  exp=$(sed -n '1p' "${CACHE_FILE}" 2>/dev/null || echo 0)
  tok=$(sed -n '2p' "${CACHE_FILE}" 2>/dev/null || echo "")
  if [ -n "${tok}" ] && [ "${exp}" -gt "$(( $(date +%s) + REFRESH_SKEW ))" ] 2>/dev/null; then
    emit_token "${tok}"
    exit 0
  fi
fi

# ── Mint a fresh App JWT ──────────────────────────────────────────────────────
now=$(date +%s)
# iat backdated 60s to tolerate clock skew; exp capped well under GitHub's 10m.
header='{"alg":"RS256","typ":"JWT"}'
payload=$(printf '{"iat":%d,"exp":%d,"iss":"%s"}' "$((now - 60))" "$((now + 540))" "${GITHUB_APP_ID}")

signing_input="$(printf '%s' "${header}" | b64url).$(printf '%s' "${payload}" | b64url)"
signature=$(printf '%s' "${signing_input}" \
  | openssl dgst -sha256 -sign <(printf '%s' "${GITHUB_APP_PRIVATE_KEY}") \
  | b64url)
jwt="${signing_input}.${signature}"

# ── Resolve installation ID dynamically ──────────────────────────────────────
# Prefer the repo-level endpoint when we have a full owner/repo; fall back to
# the org-level endpoint when only the org is known (e.g. gh wrapper path).
if [ -n "${repo}" ] && [ "${repo}" != "${owner}" ]; then
  install_resp=$(curl -fsS \
    -H "Authorization: Bearer ${jwt}" \
    -H "Accept: application/vnd.github+json" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    "${API}/repos/${owner}/${repo}/installation" 2>/dev/null || echo "")
else
  install_resp=$(curl -fsS \
    -H "Authorization: Bearer ${jwt}" \
    -H "Accept: application/vnd.github+json" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    "${API}/orgs/${owner}/installation" 2>/dev/null || echo "")
fi

install_id=$(printf '%s' "${install_resp}" | jq -r '.id // empty')

if [ -z "${install_id}" ]; then
  echo "git-credential-platform-bot: could not resolve installation for ${owner}" >&2
  exit 1
fi

# ── Mint installation access token ───────────────────────────────────────────
response=$(curl -fsS -X POST \
  -H "Authorization: Bearer ${jwt}" \
  -H "Accept: application/vnd.github+json" \
  -H "X-GitHub-Api-Version: 2022-11-28" \
  "${API}/app/installations/${install_id}/access_tokens")

token=$(printf '%s' "${response}" | jq -r '.token // empty')
expires_at=$(printf '%s' "${response}" | jq -r '.expires_at // empty')

if [ -z "${token}" ]; then
  echo "git-credential-platform-bot: failed to mint installation token for ${owner}" >&2
  exit 1
fi

exp_epoch=$(date -d "${expires_at}" +%s 2>/dev/null || echo "$((now + 3600))")
mkdir -p "${CACHE_DIR}"
umask 077
printf '%s\n%s\n' "${exp_epoch}" "${token}" > "${CACHE_FILE}"

emit_token "${token}"

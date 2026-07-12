#!/usr/bin/env bash
#
# gh wrapper — inject a fresh Enterprise Bot installation token per invocation.
# Resolves the installation dynamically based on the current git remote's org,
# so no hardcoded installation ID is needed.
#
# Installed at /usr/local/bin/gh, which precedes the real binary (/usr/bin/gh)
# on PATH. Falls through to the real gh unchanged when app creds are absent or
# when the caller already set GH_TOKEN/GITHUB_TOKEN explicitly.

set -euo pipefail

REAL_GH=/usr/bin/gh

if [ -x "${REAL_GH}" ] \
  && [ -z "${GH_TOKEN:-}" ] && [ -z "${GITHUB_TOKEN:-}" ] \
  && [ -n "${GITHUB_APP_ID:-}" ] && [ -n "${GITHUB_APP_PRIVATE_KEY:-}" ]; then

  # Detect owner/repo from the current directory's git remote so the credential
  # helper can resolve the correct per-org installation without a hardcoded ID.
  remote_url=$(git config --get remote.origin.url 2>/dev/null || echo "")
  owner="" repo_name=""
  if [ -n "$remote_url" ]; then
    stripped=$(printf '%s' "$remote_url" | sed 's|.*github\.com[:/]||' | sed 's|\.git$||')
    owner="${stripped%%/*}"
    repo_name="${stripped#*/}"
  fi

  # Build credential helper input. Include path= when we have owner/repo;
  # the helper falls back to GITHUB_DEFAULT_ORG when path= is absent.
  if [ -n "$owner" ] && [ -n "$repo_name" ] && [ "$owner" != "$repo_name" ]; then
    cred_input=$(printf 'protocol=https\nhost=github.com\npath=%s/%s\n\n' "$owner" "$repo_name")
  else
    cred_input=$(printf 'protocol=https\nhost=github.com\n\n')
  fi

  token=$(printf '%s' "$cred_input" \
    | /usr/local/bin/git-credential-platform-bot get 2>/dev/null \
    | sed -n 's/^password=//p' || true)

  if [ -n "${token}" ]; then
    exec env GH_TOKEN="${token}" "${REAL_GH}" "$@"
  fi
fi

exec "${REAL_GH}" "$@"

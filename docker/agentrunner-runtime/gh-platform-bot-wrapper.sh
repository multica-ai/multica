#!/usr/bin/env bash
#
# gh wrapper — inject a fresh Platform Bot installation token per invocation so
# `gh` never depends on a long-lived stored token. Installed at
# /usr/local/bin/gh, which precedes the real binary (/usr/bin/gh) on PATH.
#
# Like git's credential helper, this mints on demand (reusing the helper's
# short-lived cache), so gh keeps working for the entire multi-month life of the
# pod with nothing to supervise or refresh on a timer.
#
# Falls through to the real gh unchanged when app creds are absent or when the
# caller already set GH_TOKEN/GITHUB_TOKEN explicitly.

set -euo pipefail

REAL_GH=/usr/bin/gh

if [ -x "${REAL_GH}" ] \
  && [ -z "${GH_TOKEN:-}" ] && [ -z "${GITHUB_TOKEN:-}" ] \
  && [ -n "${GITHUB_APP_ID:-}" ] && [ -n "${GITHUB_APP_INSTALLATION_ID:-}" ] && [ -n "${GITHUB_APP_PRIVATE_KEY:-}" ]; then
  token=$(printf 'protocol=https\nhost=github.com\n\n' \
    | /usr/local/bin/git-credential-platform-bot get 2>/dev/null \
    | sed -n 's/^password=//p' || true)
  if [ -n "${token}" ]; then
    exec env GH_TOKEN="${token}" "${REAL_GH}" "$@"
  fi
fi

exec "${REAL_GH}" "$@"

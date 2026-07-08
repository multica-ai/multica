#!/usr/bin/env bash
# Fork install entry point — skips Homebrew and delegates to install.sh.
#
# Repo identity resolution (first match wins):
#   1. MULTICA_GITHUB_REPO already set in the environment
#   2. Positional arg: ./install-fork.sh owner/repo
#   3. Derived from `git remote get-url origin` in a local checkout
#   4. Thin fork overlay: FORK_DEFAULT_GITHUB_REPO below (empty = require 1–3)
#
# Usage (any fork — preferred):
#   MULTICA_GITHUB_REPO=owner/repo curl -fsSL \
#     https://raw.githubusercontent.com/owner/repo/main/scripts/install-fork.sh | bash
#
# Local clone:
#   ./scripts/install-fork.sh
#
set -euo pipefail

# Thin fork overlay for bare `curl|bash` without MULTICA_GITHUB_REPO.
# Leave empty when contributing upstreamable patches; set to this fork's
# owner/repo so existing one-liners keep working. Other forks should change
# this to their slug (or always pass MULTICA_GITHUB_REPO).
FORK_DEFAULT_GITHUB_REPO="Git-on-my-level/multica"

export MULTICA_SKIP_BREW=1
export MULTICA_CLI_REF="${MULTICA_CLI_REF:-main}"
export MULTICA_GITHUB_BRANCH="${MULTICA_GITHUB_BRANCH:-main}"

derive_repo_from_git_remote() {
  local dir="${1:-.}"
  command -v git >/dev/null 2>&1 || return 1
  local url
  url="$(git -C "$dir" remote get-url origin 2>/dev/null || true)"
  [ -n "$url" ] || return 1
  # git@github.com:owner/repo.git  or  https://github.com/owner/repo.git
  if [[ "$url" =~ github\.com[:/]([^/]+)/([^/.]+)(\.git)?$ ]]; then
    printf '%s/%s' "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}"
    return 0
  fi
  return 1
}

# Optional positional owner/repo (consumed before delegating to install.sh).
if [ -z "${MULTICA_GITHUB_REPO:-}" ] && [ "${1:-}" != "" ] && [[ "$1" == */* ]] && [[ "$1" != -* ]]; then
  export MULTICA_GITHUB_REPO="$1"
  shift
fi

if [ -z "${MULTICA_GITHUB_REPO:-}" ]; then
  _derived=""
  if [ -n "${BASH_SOURCE[0]:-}" ] && [ -f "${BASH_SOURCE[0]}" ]; then
    _dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    _repo_root="$(cd "${_dir}/.." && pwd)"
    if _derived="$(derive_repo_from_git_remote "$_repo_root")"; then
      export MULTICA_GITHUB_REPO="$_derived"
    fi
  fi
fi

if [ -z "${MULTICA_GITHUB_REPO:-}" ] && [ -n "${FORK_DEFAULT_GITHUB_REPO}" ]; then
  export MULTICA_GITHUB_REPO="${FORK_DEFAULT_GITHUB_REPO}"
  printf 'note: using fork default MULTICA_GITHUB_REPO=%s (set the env var to override)\n' \
    "$MULTICA_GITHUB_REPO" >&2
fi

if [ -z "${MULTICA_GITHUB_REPO:-}" ]; then
  printf '%s\n' \
    "error: could not determine the GitHub repo for this fork." \
    "Set MULTICA_GITHUB_REPO=owner/repo and re-run, for example:" \
    "  MULTICA_GITHUB_REPO=owner/repo curl -fsSL https://raw.githubusercontent.com/owner/repo/main/scripts/install-fork.sh | bash" \
    >&2
  exit 1
fi

repo="${MULTICA_GITHUB_REPO}"
branch="${MULTICA_GITHUB_BRANCH}"

# Local checkout: install.sh sits beside this wrapper.
if [ -n "${BASH_SOURCE[0]:-}" ] && [ -f "${BASH_SOURCE[0]:-}" ]; then
  _dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  if [ -f "${_dir}/install.sh" ]; then
    exec bash "${_dir}/install.sh" "$@"
  fi
fi

# curl .../install-fork.sh | bash — fetch install.sh from the fork repo.
exec bash <(curl -fsSL "https://raw.githubusercontent.com/${repo}/${branch}/scripts/install.sh") "$@"

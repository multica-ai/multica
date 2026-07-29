#!/usr/bin/env bash
#
# gh wrapper — inject a fresh Enterprise Bot installation token per invocation.
# Resolves the installation dynamically based on the target repo, so no
# hardcoded installation ID is needed.
#
# Installed at /usr/local/bin/gh, which precedes the real binary (/usr/bin/gh)
# on PATH. Falls through to the real gh unchanged when app creds are absent or
# when the caller already set GH_TOKEN/GITHUB_TOKEN explicitly.

set -euo pipefail

REAL_GH=/usr/bin/gh

if [ -x "${REAL_GH}" ] \
  && [ -z "${GH_TOKEN:-}" ] && [ -z "${GITHUB_TOKEN:-}" ] \
  && [ -n "${GITHUB_APP_ID:-}" ] && [ -n "${GITHUB_APP_PRIVATE_KEY:-}" ]; then

  owner="" repo_name="" repo_spec=""

  # 1. An explicit -R/--repo target (accepted by nearly every gh subcommand)
  #    names the real target regardless of cwd — trust it over any cwd guess.
  prev=""
  for arg in "$@"; do
    case "$prev" in
      -R|--repo) repo_spec="$arg" ;;
    esac
    case "$arg" in
      --repo=*) repo_spec="${arg#--repo=}" ;;
    esac
    prev="$arg"
  done

  # 2. `gh api repos/OWNER/REPO...` and `gh repo view|clone|fork OWNER/REPO`
  #    name the target repo directly in a positional argument. Match on the
  #    distinctive "repos/" prefix (gh api) or a bare OWNER/REPO positional
  #    under `gh repo ...` — narrow enough not to misfire on flag values.
  if [ -z "$repo_spec" ] && [ "${1:-}" = "api" ]; then
    for arg in "${@:2}"; do
      case "$arg" in
        repos/*/*)
          rest="${arg#repos/}"
          repo_spec="${rest%%/*}/$(printf '%s' "${rest#*/}" | cut -d/ -f1)"
          break
          ;;
      esac
    done
  elif [ -z "$repo_spec" ] && [ "${1:-}" = "repo" ]; then
    for arg in "${@:2}"; do
      case "$arg" in
        -*) ;;
        */*) repo_spec="$arg"; break ;;
      esac
    done
  fi

  if [ -n "$repo_spec" ]; then
    stripped="${repo_spec#*github.com/}"
    stripped="${stripped%.git}"
    owner="${stripped%%/*}"
    repo_name="${stripped#*/}"
  else
    # Fall back to the current directory's git remote so the credential
    # helper can still resolve the correct per-org installation when the
    # command line doesn't name a target repo (e.g. `gh pr list` run inside
    # a checkout).
    remote_url=$(git config --get remote.origin.url 2>/dev/null || echo "")
    if [ -n "$remote_url" ]; then
      stripped=$(printf '%s' "$remote_url" | sed 's|.*github\.com[:/]||' | sed 's|\.git$||')
      owner="${stripped%%/*}"
      repo_name="${stripped#*/}"
    fi
  fi

  # Build credential helper input. Include path= when we have owner/repo;
  # the helper falls back to GITHUB_DEFAULT_ORG when path= is absent.
  #
  # Pipe printf directly into the credential helper rather than round-tripping
  # through a $(...) capture — command substitution strips all trailing
  # newlines, which used to drop the final path= line before the helper ever
  # saw it (it silently fell back to GITHUB_DEFAULT_ORG for every repo outside
  # that org).
  if [ -n "$owner" ] && [ -n "$repo_name" ] && [ "$owner" != "$repo_name" ]; then
    token=$(printf 'protocol=https\nhost=github.com\npath=%s/%s\n\n' "$owner" "$repo_name" \
      | /usr/local/bin/git-credential-platform-bot get 2>/dev/null \
      | sed -n 's/^password=//p' || true)
  else
    token=$(printf 'protocol=https\nhost=github.com\n\n' \
      | /usr/local/bin/git-credential-platform-bot get 2>/dev/null \
      | sed -n 's/^password=//p' || true)
  fi

  if [ -n "${token}" ]; then
    exec env GH_TOKEN="${token}" "${REAL_GH}" "$@"
  fi
fi

exec "${REAL_GH}" "$@"

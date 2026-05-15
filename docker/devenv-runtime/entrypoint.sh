#!/bin/bash
# devenv-runtime entrypoint — starts the opencode HTTP server bound to
# OPENCODE_HOST:OPENCODE_PORT so the per-developer Kubernetes Service can
# reach it.
#
# Required env (set by the Deployment / Kustomize overlay): none.
# Optional env:
#   GH_TOKEN      — GitHub PAT. When set, `gh` is wired as git's credential
#                   helper so `git clone` over HTTPS works for private repos.
#   MULTICA_TOKEN — Multica personal access token (mul_...). When set, the
#                   multica CLI is configured for the self-hosted server and
#                   the daemon is started in the background so the devenv
#                   joins the Multica workspace.
#   MULTICA_SERVER_URL — Multica server URL (default https://agentfarm.g2.com).
#   MULTICA_APP_URL    — Multica web app URL (default https://agentfarm.g2.com).
#   OPENCODE_HOST — bind address (default 0.0.0.0; see Dockerfile ENV).
#   OPENCODE_PORT — bind port    (default 4096;    see Dockerfile ENV).
#   OPENCODE_EXTRA_ARGS — extra args appended verbatim, e.g.
#                         `--cors https://devenv-jshuff.development.g2.com`.

set -euo pipefail

# Wire `gh` as git's credential helper for github.com so plain `git clone`
# uses GH_TOKEN; also rewrite SSH-style remotes to HTTPS to route through it.
if [ -n "${GH_TOKEN:-}" ]; then
  if gh auth setup-git --hostname github.com; then
    git config --global --unset-all url."https://github.com/".insteadOf 2>/dev/null || true
    git config --global --add url."https://github.com/".insteadOf "git@github.com:"
    git config --global --add url."https://github.com/".insteadOf "ssh://git@github.com/"
  fi
fi

# ── Multica daemon (optional) ───────────────────────────────────
# When MULTICA_TOKEN is set, configure the CLI for the self-hosted
# server and start the daemon in the background. The daemon connects
# the devenv to the Multica workspace so agents can be dispatched.
if [ -n "${MULTICA_TOKEN:-}" ]; then
  server_url="${MULTICA_SERVER_URL:-https://agentfarm.g2.com}"
  app_url="${MULTICA_APP_URL:-https://agentfarm.g2.com}"

  # Write config directly — avoids the interactive prompts in
  # `multica setup self-host` and `multica login`.
  config_dir="$HOME/.multica"
  config_file="$config_dir/config.json"
  mkdir -p "$config_dir"
  umask 077
  cat > "$config_file" <<JSON
{
  "server_url": "${server_url}",
  "app_url": "${app_url}",
  "token": "${MULTICA_TOKEN}"
}
JSON

  echo "devenv: multica configured for ${server_url}"
  multica auth status || true

  # Start daemon in background — opencode serve is still PID 1 (via exec).
  multica daemon start &
  echo "devenv: multica daemon started (pid $!)"
fi

host="${OPENCODE_HOST:-0.0.0.0}"
port="${OPENCODE_PORT:-4096}"

# shellcheck disable=SC2086  # word-splitting on OPENCODE_EXTRA_ARGS is intentional
exec opencode serve \
  --hostname "$host" \
  --port "$port" \
  ${OPENCODE_EXTRA_ARGS:-}

#!/bin/bash
# devenv-runtime entrypoint — starts the opencode HTTP server bound to
# OPENCODE_HOST:OPENCODE_PORT so the per-developer Kubernetes Service can
# reach it.
#
# Required env (set by the Deployment / Kustomize overlay): none.
# Optional env:
#   GH_TOKEN      — GitHub PAT. When set, `gh` is wired as git's credential
#                   helper so `git clone` over HTTPS works for private repos.
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

host="${OPENCODE_HOST:-0.0.0.0}"
port="${OPENCODE_PORT:-4096}"

# shellcheck disable=SC2086  # word-splitting on OPENCODE_EXTRA_ARGS is intentional
exec opencode serve \
  --hostname "$host" \
  --port "$port" \
  ${OPENCODE_EXTRA_ARGS:-}

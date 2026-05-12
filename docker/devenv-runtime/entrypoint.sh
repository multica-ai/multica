#!/bin/bash
# devenv-runtime entrypoint — starts the opencode HTTP server bound to
# OPENCODE_HOST:OPENCODE_PORT so the per-developer Kubernetes Service can
# reach it.
#
# Required env (set by the Deployment / Kustomize overlay): none.
# Optional env:
#   OPENCODE_HOST — bind address (default 0.0.0.0; see Dockerfile ENV).
#   OPENCODE_PORT — bind port    (default 4096;    see Dockerfile ENV).
#   OPENCODE_EXTRA_ARGS — extra args appended verbatim, e.g.
#                         `--cors https://devenv-jshuff.development.g2.com`.

set -euo pipefail

host="${OPENCODE_HOST:-0.0.0.0}"
port="${OPENCODE_PORT:-4096}"

# shellcheck disable=SC2086  # word-splitting on OPENCODE_EXTRA_ARGS is intentional
exec opencode serve \
  --hostname "$host" \
  --port "$port" \
  ${OPENCODE_EXTRA_ARGS:-}

#!/usr/bin/env bash
# Multica Compose wrapper — forwards to `docker compose` or `podman compose`.
# Override: MULTICA_COMPOSE="podman compose" (whitespace-separated prefix; no spaces in binary path).
set -euo pipefail

die() {
  printf '%s\n' "multica: $*" >&2
  printf '%s\n' "Install Docker Engine with Compose v2 (https://docs.docker.com/engine/install/) or Podman 4+ with compose (https://podman.io/docs/installation)." >&2
  exit 1
}

main() {
  local -a cmd

  if [[ -n "${MULTICA_COMPOSE:-}" ]]; then
    # shellcheck disable=SC2206
    cmd=( ${MULTICA_COMPOSE} )
    if [[ ${#cmd[@]} -eq 0 ]]; then
      die "MULTICA_COMPOSE is set but empty."
    fi
    exec "${cmd[@]}" "$@"
  fi

  if command -v docker >/dev/null 2>&1 \
    && docker info >/dev/null 2>&1 \
    && docker compose version >/dev/null 2>&1; then
    exec docker compose "$@"
  fi

  if command -v podman >/dev/null 2>&1 \
    && podman compose version >/dev/null 2>&1; then
    exec podman compose "$@"
  fi

  die "No working compose backend found (tried docker compose, then podman compose)."
}

main "$@"

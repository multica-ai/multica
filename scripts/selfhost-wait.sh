#!/usr/bin/env bash
# Wait for the self-hosted stack to answer /health, then print connection info.
# Shared by `make selfhost` and `make selfhost-build`.
#
# Why the host port is read back from Compose instead of re-derived here: the
# recipes used to probe `${PORT:-8080}` while Compose published `${BACKEND_PORT:-8080}`.
# Two different variables as the source of truth means any config where they
# disagree breaks — `.env` setting PORT with BACKEND_PORT unset probed the custom
# port while the stack sat on 8080, and `make selfhost PORT=8080` over a .env
# with BACKEND_PORT=9100 probed 8080 while the stack sat on 9100, hammering the
# wrong port for 60s and then reporting "Services are still starting" on a
# healthy install. `docker compose port` is the only authority on what Compose
# actually published, so ask it rather than re-deriving the answer.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

mode=${1:-official}
case "$mode" in
official)
  compose_files=(-f docker-compose.selfhost.yml)
  ;;
build)
  compose_files=(-f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml)
  ;;
*)
  echo "usage: ${BASH_SOURCE[0]} [official|build]" >&2
  exit 2
  ;;
esac

# The Makefile exports COMPOSE; keep the same default when run standalone.
read -r -a compose_cmd <<<"${COMPOSE:-docker compose}"

# Published host port for a service, straight from Compose. Falls back to the
# same default chain as docker-compose.selfhost.yml when the container is not up
# (e.g. `up -d` failed) so the message degrades instead of breaking.
compose_host_port() {
  local service=$1 container_port=$2 fallback=$3 published

  published=$("${compose_cmd[@]}" "${compose_files[@]}" port "$service" "$container_port" 2>/dev/null | tail -n 1) || published=""
  published=${published##*:}
  published=${published%$'\r'}

  case "$published" in
  '' | *[!0-9]*) printf '%s\n' "$fallback" ;;
  *) printf '%s\n' "$published" ;;
  esac
}

backend_port=$(compose_host_port backend 8080 "${BACKEND_PORT:-${API_PORT:-${SERVER_PORT:-${PORT:-8080}}}}")
frontend_port=$(compose_host_port frontend 3000 "${FRONTEND_PORT:-3000}")

backend_url="http://localhost:${backend_port}"
frontend_url="http://localhost:${frontend_port}"

health_ok() {
  curl -sf "${backend_url}/health" >/dev/null 2>&1
}

echo "==> Waiting for backend to be ready..."
for _ in $(seq 1 30); do
  if health_ok; then
    break
  fi
  sleep 2
done

if ! health_ok; then
  echo ""
  echo "Services are still starting. Check logs:"
  echo "  ${compose_cmd[*]} ${compose_files[*]} logs"
  exit 0
fi

echo ""
echo "✓ Multica is running!"
echo "  Frontend: ${frontend_url}"
echo "  Backend:  ${backend_url}"
echo ""
if [ "$mode" = "build" ]; then
  echo "Built images locally via docker-compose.selfhost.build.yml."
  echo "Local tags: multica-backend:dev and multica-web:dev."
else
  echo "Images: ${MULTICA_BACKEND_IMAGE:-ghcr.io/multica-ai/multica-backend}:${MULTICA_IMAGE_TAG:-latest}"
  echo "        ${MULTICA_WEB_IMAGE:-ghcr.io/multica-ai/multica-web}:${MULTICA_IMAGE_TAG:-latest}"
fi
echo ""
echo "Log in: configure RESEND_API_KEY in .env for email codes,"
echo "        or read the generated code from backend logs when Resend is unset."
echo ""
echo "Next — install the CLI and connect your machine:"
echo "  brew install multica-ai/tap/multica"
echo "  multica setup self-host"

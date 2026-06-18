#!/usr/bin/env bash
set -euo pipefail
ENV_FILE="${ENV_FILE:-.env}"
[ -f "$ENV_FILE" ] || cp .env.example "$ENV_FILE"

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

# shellcheck disable=SC1091
. scripts/selfhost-env.sh

SELFHOST_START_DAEMON="${SELFHOST_START_DAEMON:-false}"
SELFHOST_MAX_ATTEMPTS="${SELFHOST_MAX_ATTEMPTS:-60}"
SELFHOST_WAIT_SECONDS="${SELFHOST_WAIT_SECONDS:-2}"

diagnose_startup() {
  echo ""
  echo "Self-host startup did not become ready."
  echo "Quick checks:"
  echo "  - backend readiness: ${BACKEND_BASE_URL}/readyz"
  echo "  - frontend:          ${FRONTEND_BASE_URL}/"
  echo "  - logs:              docker compose --env-file ${ENV_FILE} -f docker-compose.selfhost.yml logs backend frontend postgres"
  echo ""
  echo "Common causes:"
  echo "  - image pull failed or the selected MULTICA_IMAGE_TAG is unpublished"
  echo "  - host port ${BACKEND_PORT} or ${FRONTEND_PORT} is already occupied"
  echo "  - Postgres is not healthy, migrations failed, or /readyz reports DB/migration not ready"
  echo "  - frontend container is still starting or failed to boot"
}

check_url() {
  curl -fsS --max-time 2 "$1" >/dev/null 2>&1
}

wait_for_url() {
  local label=$1
  local url=$2

  echo "==> Waiting for ${label}: ${url}"
  for _ in $(seq 1 "$SELFHOST_MAX_ATTEMPTS"); do
    if check_url "$url"; then
      echo "==> ${label} is ready"
      return 0
    fi
    sleep "$SELFHOST_WAIT_SECONDS"
  done

  diagnose_startup
  return 1
}

echo "==> Pulling Multica images..."
if ! docker compose --env-file "$ENV_FILE" -f docker-compose.selfhost.yml pull; then
  echo "Image pull failed. If this tag is not published yet, use:"
  echo "  docker compose --env-file ${ENV_FILE} -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml up -d --build"
  exit 1
fi

echo "==> Starting Multica server stack..."
if ! docker compose --env-file "$ENV_FILE" -f docker-compose.selfhost.yml up -d; then
  echo "Docker Compose failed to start. Check whether host ports ${BACKEND_PORT}/${FRONTEND_PORT} are already occupied."
  exit 1
fi

wait_for_url "backend readiness" "${BACKEND_BASE_URL}/readyz"
wait_for_url "frontend" "${FRONTEND_BASE_URL}/"

echo ""
echo "Multica server stack is ready."
echo "  Frontend: ${FRONTEND_BASE_URL}"
echo "  Backend:  ${BACKEND_BASE_URL}"
echo ""
echo "Next: configure the CLI and start the daemon:"
echo "  multica setup self-host --server-url ${BACKEND_BASE_URL} --app-url ${FRONTEND_BASE_URL}"

if [ "$SELFHOST_START_DAEMON" = "true" ]; then
  echo ""
  echo "==> SELFHOST_START_DAEMON=true; starting daemon from source..."
  cd server
  CGO_ENABLED=0 MULTICA_SERVER_URL="$MULTICA_SERVER_URL" go run ./cmd/multica daemon start
fi

#!/usr/bin/env bash
set -euo pipefail

# Build backend/web from the current checkout as an isolated self-host preview.
# Intended for parallel Issue QA: it derives a stable preview profile from
# ISSUE/PROFILE, finds free host ports, and keeps DB cleanup out of the preview
# lifecycle so local test data survives human acceptance.

if [ ! -f .env ]; then
  echo "==> Creating .env from .env.example..."
  cp .env.example .env
  jwt="$(openssl rand -hex 32)"
  if [ "$(uname)" = "Darwin" ]; then
    sed -i '' "s/^JWT_SECRET=.*/JWT_SECRET=${jwt}/" .env
  else
    sed -i "s/^JWT_SECRET=.*/JWT_SECRET=${jwt}/" .env
  fi
  echo "==> Generated random JWT_SECRET"
fi

base_frontend_port="${FRONTEND_PORT:-3000}"
base_backend_port="${BACKEND_PORT:-${API_PORT:-${SERVER_PORT:-${PORT:-8080}}}}"
postgres_port="${POSTGRES_PORT:-5432}"
postgres_user="${POSTGRES_USER:-multica}"
postgres_password="${POSTGRES_PASSWORD:-multica}"

is_port_free() {
  local port="$1"
  python3 - "$port" <<'PY'
import socket, sys
port = int(sys.argv[1])
with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
    s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    try:
        s.bind(("127.0.0.1", port))
    except OSError:
        sys.exit(1)
PY
}

next_free_port() {
  local port="$1"
  while ! is_port_free "$port"; do
    port=$((port + 1))
  done
  printf '%s\n' "$port"
}

slugify() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/_/g; s/__*/_/g; s/^_//; s/_$//'
}

issue="${ISSUE:-}"
if [ -n "$issue" ]; then
  issue="$(printf '%s' "$issue" | tr '[:lower:]' '[:upper:]')"
fi

if [ -n "${PROFILE:-}" ]; then
  profile="$(slugify "$PROFILE")"
elif [ -n "$issue" ]; then
  profile="$(slugify "$issue")"
else
  profile="$(slugify "${WORKTREE_NAME:-$(basename "$PWD")}")"
fi
if [ -z "$profile" ]; then
  profile="multica"
fi

project_name="${COMPOSE_PROJECT_NAME:-multica_preview_${profile}}"

# Guard: skip rebuild if this profile already has running services
echo "==> Checking if preview '${profile}' is already running..."
if docker ps --filter "label=com.docker.compose.project=${project_name}" -q 2>/dev/null | grep -q .; then
  echo ""
  echo "⚠️  Preview '${profile}' already has running services."
  echo "  To rebuild, stop it first:"
  if [ -n "$issue" ]; then
    echo "    make selfhost-preview-clean ISSUE=${issue}"
  elif [ -n "${PROFILE:-}" ]; then
    echo "    make selfhost-preview-clean PROFILE=${PROFILE}"
  else
    echo "    make selfhost-preview-clean"
  fi
  exit 0
fi

# Clean up any leftover containers (stopped/exited) before allocating ports
echo "==> Stopping any previous preview containers for '${profile}'..."
if ! COMPOSE_PROJECT_NAME="$project_name" \
  docker compose \
    -p "$project_name" \
    -f docker-compose.selfhost.yml \
    -f docker-compose.selfhost.build.yml \
    down; then
  echo "❌ Failed to stop previous preview containers. Check docker status and retry." >&2
  exit 1
fi

frontend_port="$(next_free_port "$base_frontend_port")"
backend_port="$(next_free_port "$base_backend_port")"

if [ "${ISOLATED_DB:-0}" = "1" ]; then
  db_name="${POSTGRES_DB:-multica_preview_${profile}}"
else
  db_name="${POSTGRES_DB:-multica}"
fi
frontend_origin="http://localhost:${frontend_port}"
backend_origin="http://localhost:${backend_port}"
database_url="postgres://${postgres_user}:${postgres_password}@host.docker.internal:${postgres_port}/${db_name}?sslmode=disable"
host_database_url="postgres://${postgres_user}:${postgres_password}@localhost:${postgres_port}/${db_name}?sslmode=disable"

tmp_override="$(mktemp -t multica-selfhost-preview.XXXXXX.yml)"
cleanup() {
  rm -f "$tmp_override"
}
trap cleanup EXIT

cat > "$tmp_override" <<EOF
services:
  backend:
    depends_on: {}
    environment:
      DATABASE_URL: ${database_url}
      FRONTEND_ORIGIN: ${frontend_origin}
      CORS_ALLOWED_ORIGINS: ${frontend_origin}
      ALLOWED_ORIGINS: ${frontend_origin}
      GOOGLE_REDIRECT_URI: ${frontend_origin}/auth/callback
      MULTICA_APP_URL: ${frontend_origin}
      LOCAL_UPLOAD_BASE_URL: ${backend_origin}

  frontend:
    build:
      args:
        REMOTE_API_URL: http://backend:8080
        NEXT_PUBLIC_WS_URL: ws://localhost:${backend_port}/ws
        NEXT_PUBLIC_APP_VERSION: dev
EOF

echo "==> Ensuring shared PostgreSQL container is running on localhost:${postgres_port}..."
docker compose up -d postgres

echo "==> Waiting for PostgreSQL to be ready..."
until docker compose exec -T postgres pg_isready -U "$postgres_user" -d postgres > /dev/null 2>&1; do
  sleep 1
done

echo "==> Ensuring database '$db_name' exists..."
db_exists="$(docker compose exec -T postgres \
  psql -U "$postgres_user" -d postgres -Atqc "SELECT 1 FROM pg_database WHERE datname = '$db_name'")"

if [ "$db_exists" != "1" ]; then
  docker compose exec -T postgres \
    psql -U "$postgres_user" -d postgres -v ON_ERROR_STOP=1 \
    -c "CREATE DATABASE \"$db_name\"" \
    > /dev/null
fi

echo "==> Building Multica preview from the current checkout..."
COMPOSE_PROJECT_NAME="$project_name" \
POSTGRES_DB="$db_name" \
POSTGRES_USER="$postgres_user" \
POSTGRES_PASSWORD="$postgres_password" \
POSTGRES_PORT="$postgres_port" \
DATABASE_URL="$host_database_url" \
BACKEND_PORT="$backend_port" \
PORT="$backend_port" \
FRONTEND_PORT="$frontend_port" \
FRONTEND_ORIGIN="$frontend_origin" \
MULTICA_APP_URL="$frontend_origin" \
GOOGLE_REDIRECT_URI="$frontend_origin/auth/callback" \
NEXT_PUBLIC_WS_URL="ws://localhost:${backend_port}/ws" \
docker compose \
  -p "$project_name" \
  -f docker-compose.selfhost.yml \
  -f docker-compose.selfhost.build.yml \
  -f "$tmp_override" \
  up -d --no-deps --build backend frontend

echo "==> Waiting for backend to be ready..."
for _ in $(seq 1 60); do
  if curl -sf "${backend_origin}/health" > /dev/null 2>&1; then
    break
  fi
  sleep 2
done

if curl -sf "${backend_origin}/health" > /dev/null 2>&1; then
  echo ""
  echo "✓ Multica preview is running!"
  if [ -n "$issue" ]; then
    echo "  Issue:           ${issue}"
  fi
  echo "  Preview profile: ${profile}"
  echo "  Compose project: ${project_name}"
  echo "  Database:        ${db_name} (shared Postgres on localhost:${postgres_port}; cleanup does not drop DB)"
  if [ "${ISOLATED_DB:-0}" != "1" ]; then
    other_previews="$(docker ps --filter "label=com.docker.compose.project" --format '{{.Labels}}' 2>/dev/null \
      | grep -o 'com.docker.compose.project=[^ ,]*' | cut -d= -f2 | sort -u \
      | grep '^multica_preview_' | grep -v "^${project_name}$" || true)"
    if [ -n "$other_previews" ]; then
      echo ""
      echo "  ⚠️  Other preview projects detected sharing the same DB:"
      echo "$other_previews" | sed 's/^/     - /'
      echo "  If any of them run migrations, consider: ISOLATED_DB=1 make selfhost-build-preview ISSUE=..."
    fi
  fi
  echo "  Frontend:        ${frontend_origin}"
  echo "  Backend:         ${backend_origin}"
  echo ""
  echo "Stop this preview:"
  if [ -n "$issue" ]; then
    echo "  make selfhost-preview-clean ISSUE=${issue}"
  elif [ -n "${PROFILE:-}" ]; then
    echo "  make selfhost-preview-clean PROFILE=${PROFILE}"
  else
    echo "  make selfhost-preview-clean"
  fi
  echo "  # or: docker compose -p ${project_name} -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml down"
else
  echo ""
  echo "Services are still starting. Check logs:"
  echo "  docker compose -p ${project_name} -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml logs"
  exit 1
fi

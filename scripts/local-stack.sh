#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="$ROOT_DIR/.env.local"
ENV_TEMPLATE="$ROOT_DIR/.env.local.example"
BASE_COMPOSE="$ROOT_DIR/docker-compose.local.yml"
AI_COMPOSE="$ROOT_DIR/docker-compose.local-ai.yml"
ACTION="${1:-up}"
CONFIRM="${2:-}"

usage() {
  cat <<'EOF'
Usage: scripts/local-stack.sh <command>

Commands:
  up       Build and start the fully local stack
  up-ai    Build and start the stack with local Ollama
  down     Stop the base and optional AI services
  restart  Recreate the base stack
  logs     Follow logs from all local services
  ps       Show service status
  config   Render the merged Docker Compose configuration
  reset    Delete containers and local data volumes (pass --yes)
EOF
}

require_docker() {
  command -v docker >/dev/null 2>&1 || {
    echo "docker is required." >&2
    exit 1
  }
  docker compose version >/dev/null 2>&1 || {
    echo "Docker Compose v2 is required." >&2
    exit 1
  }
}

random_hex() {
  local bytes=$1
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex "$bytes"
  else
    od -An -N "$bytes" -tx1 /dev/urandom | tr -d ' \n'
  fi
}

random_base64() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 32 | tr -d '\n'
  else
    head -c 32 /dev/urandom | base64 | tr -d '\n'
  fi
}

set_env_value() {
  local key=$1
  local value=$2
  local temp
  temp="$(mktemp)"
  awk -v key="$key" -v value="$value" '
    BEGIN { replaced = 0 }
    index($0, key "=") == 1 { print key "=" value; replaced = 1; next }
    { print }
    END { if (!replaced) print key "=" value }
  ' "$ENV_FILE" >"$temp"
  mv "$temp" "$ENV_FILE"
}

init_env() {
  local initialized=0
  if [[ ! -f "$ENV_FILE" ]]; then
    cp "$ENV_TEMPLATE" "$ENV_FILE"
    initialized=1
  fi

  if grep -q '^POSTGRES_PASSWORD=replace-with-' "$ENV_FILE"; then
    set_env_value POSTGRES_PASSWORD "$(random_hex 24)"
    initialized=1
  fi
  if grep -q '^JWT_SECRET=replace-with-' "$ENV_FILE"; then
    set_env_value JWT_SECRET "$(random_hex 32)"
    initialized=1
  fi
  if grep -q '^MULTICA_VCS_SECRET_KEY=replace-with-' "$ENV_FILE"; then
    set_env_value MULTICA_VCS_SECRET_KEY "$(random_base64)"
    initialized=1
  fi
  if [[ "$initialized" == "1" ]]; then
    echo "Initialized .env.local with random local secrets."
  fi
}

compose_base=(docker compose --env-file "$ENV_FILE" -f "$BASE_COMPOSE")
compose_all=(docker compose --env-file "$ENV_FILE" -f "$BASE_COMPOSE" -f "$AI_COMPOSE")

require_docker
init_env
cd "$ROOT_DIR"

case "$ACTION" in
  up)
    "${compose_base[@]}" up -d --build --wait
    echo "Multica: http://localhost:$(sed -n 's/^FRONTEND_PORT=//p' "$ENV_FILE")"
    echo "Mailpit: http://localhost:$(sed -n 's/^MAILPIT_UI_PORT=//p' "$ENV_FILE")"
    ;;
  up-ai)
    "${compose_all[@]}" up -d --build --wait
    echo "Multica and Ollama are ready."
    echo "Multica: http://localhost:$(sed -n 's/^FRONTEND_PORT=//p' "$ENV_FILE")"
    echo "Mailpit: http://localhost:$(sed -n 's/^MAILPIT_UI_PORT=//p' "$ENV_FILE")"
    ;;
  down)
    "${compose_all[@]}" down --remove-orphans
    ;;
  restart)
    "${compose_base[@]}" up -d --build --force-recreate --wait
    ;;
  logs)
    "${compose_all[@]}" logs -f --tail=200
    ;;
  ps)
    "${compose_all[@]}" ps
    ;;
  config)
    "${compose_all[@]}" config
    ;;
  reset)
    if [[ "$CONFIRM" != "--yes" ]]; then
      echo "Run 'scripts/local-stack.sh reset --yes' to delete all local data volumes." >&2
      exit 2
    fi
    "${compose_all[@]}" down --volumes --remove-orphans
    ;;
  help|-h|--help)
    usage
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac

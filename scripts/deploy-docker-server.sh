#!/usr/bin/env bash
set -euo pipefail

# Resolve paths relative to the repository root so the script can run anywhere.
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Deployment settings can be overridden from the shell without editing this file.
IMAGE_NAME="${IMAGE_NAME:-multim-server}"
CONTAINER_NAME="${CONTAINER_NAME:-multim-server}"
DOCKER_NETWORK="${DOCKER_NETWORK:-1panel-network}"
HOST_PORT="${HOST_PORT:-10626}"
CONTAINER_PORT="${CONTAINER_PORT:-8080}"
ENV_FILE="${ENV_FILE:-deploy/docker-server.env}"
HEALTH_PATH="${HEALTH_PATH:-/health}"
HEALTH_TIMEOUT_SECONDS="${HEALTH_TIMEOUT_SECONDS:-30}"
SKIP_PULL="${SKIP_PULL:-0}"
SKIP_HEALTH="${SKIP_HEALTH:-0}"
CLEANUP_IMAGES="${CLEANUP_IMAGES:-1}"
DRY_RUN="${DRY_RUN:-0}"
VALIDATE_ONLY="${VALIDATE_ONLY:-0}"

TAG=""

# Print a consistent step heading for deployment logs.
log_step() {
  printf '\n==> %s\n' "$1"
}

# Fail early when a command required by the selected flow is unavailable.
require_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "Missing required command: $cmd" >&2
    exit 1
  fi
}

# Run a command, or only print it when DRY_RUN=1.
run_cmd() {
  if [ "$DRY_RUN" = "1" ]; then
    printf '+'
    printf ' %q' "$@"
    printf '\n'
    return
  fi

  "$@"
}

# Convert a possibly relative env file path to an absolute path.
resolve_env_file() {
  if [[ "$ENV_FILE" = /* ]]; then
    echo "$ENV_FILE"
    return
  fi

  echo "$ROOT_DIR/$ENV_FILE"
}

# Check that the env file exists and contains the variables required by the server.
validate_env_file() {
  local env_file="$1"
  local missing=0
  local key
  local required_keys=(
    APP_ENV
    PORT
    DATABASE_URL
    JWT_SECRET
    FRONTEND_ORIGIN
    CORS_ALLOWED_ORIGINS
  )

  if [ ! -f "$env_file" ]; then
    echo "Missing env file: $env_file" >&2
    echo "Create it from deploy/docker-server.env.example and fill production values." >&2
    exit 1
  fi

  for key in "${required_keys[@]}"; do
    if ! grep -Eq "^[[:space:]]*${key}=.+" "$env_file"; then
      echo "Missing required env value: $key" >&2
      missing=1
    fi
  done

  if [ "$missing" = "1" ]; then
    exit 1
  fi
}

# Ensure local tools and Docker network are ready before changing any container.
validate_runtime() {
  require_cmd git
  require_cmd docker

  if [ "$SKIP_HEALTH" != "1" ]; then
    require_cmd curl
  fi

  if [ "$DRY_RUN" != "1" ]; then
    docker network inspect "$DOCKER_NETWORK" >/dev/null
  fi
}

# Pull the latest code with fast-forward only so local changes are never overwritten.
pull_latest() {
  if [ "$SKIP_PULL" = "1" ]; then
    log_step "Skipping git pull"
    return
  fi

  log_step "Pulling latest code"
  run_cmd git -C "$ROOT_DIR" pull --ff-only
}

# Build the Docker image and tag it with both the commit SHA and latest.
build_image() {
  TAG="$(git -C "$ROOT_DIR" rev-parse --short HEAD)"

  log_step "Building Docker image: ${IMAGE_NAME}:${TAG}"
  run_cmd docker build \
    --build-arg "VERSION=${TAG}" \
    --build-arg "COMMIT=${TAG}" \
    -t "${IMAGE_NAME}:${TAG}" \
    -t "${IMAGE_NAME}:latest" \
    "$ROOT_DIR"
}

# Run database migrations from the newly built image before replacing the server.
run_migrations() {
  local env_file="$1"

  log_step "Running database migrations"
  run_cmd docker run --rm \
    --network "$DOCKER_NETWORK" \
    --env-file "$env_file" \
    --entrypoint ./migrate \
    "${IMAGE_NAME}:latest" up
}

# Replace the old server container with a new one using the same env file.
replace_container() {
  local env_file="$1"

  log_step "Replacing server container"
  if [ "$DRY_RUN" = "1" ]; then
    run_cmd docker rm -f "$CONTAINER_NAME"
  else
    docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
  fi

  run_cmd docker run -d \
    --name "$CONTAINER_NAME" \
    --restart unless-stopped \
    --network "$DOCKER_NETWORK" \
    -p "${HOST_PORT}:${CONTAINER_PORT}" \
    --env-file "$env_file" \
    "${IMAGE_NAME}:latest"
}

# Poll the local health endpoint until the new container is accepting requests.
wait_for_health() {
  local deadline=$((SECONDS + HEALTH_TIMEOUT_SECONDS))
  local health_url="http://127.0.0.1:${HOST_PORT}${HEALTH_PATH}"

  if [ "$SKIP_HEALTH" = "1" ]; then
    log_step "Skipping health check"
    return
  fi

  log_step "Waiting for health check: $health_url"
  if [ "$DRY_RUN" = "1" ]; then
    echo "DRY_RUN=1, health check not executed."
    return
  fi

  until curl -fsS "$health_url" >/dev/null; do
    if [ "$SECONDS" -ge "$deadline" ]; then
      echo "Health check failed after ${HEALTH_TIMEOUT_SECONDS}s: $health_url" >&2
      echo "Inspect logs with: docker logs --tail=200 $CONTAINER_NAME" >&2
      exit 1
    fi
    sleep 1
  done
}

# Remove dangling image layers after a successful deployment.
cleanup_images() {
  if [ "$CLEANUP_IMAGES" != "1" ]; then
    return
  fi

  log_step "Pruning dangling Docker images"
  run_cmd docker image prune -f
}

# Print operational commands that are useful immediately after deployment.
print_summary() {
  log_step "Deployment summary"
  echo "Image:      ${IMAGE_NAME}:${TAG:-unknown}"
  echo "Container:  $CONTAINER_NAME"
  echo "Port:       127.0.0.1:${HOST_PORT} -> ${CONTAINER_PORT}"
  echo "Health:     http://127.0.0.1:${HOST_PORT}${HEALTH_PATH}"
  echo ""
  echo "Logs:       docker logs -f $CONTAINER_NAME"
  echo "Rollback:   docker rm -f $CONTAINER_NAME && docker run -d --name $CONTAINER_NAME --restart unless-stopped --network $DOCKER_NETWORK -p ${HOST_PORT}:${CONTAINER_PORT} --env-file $(resolve_env_file) ${IMAGE_NAME}:<old-tag>"
}

# Coordinate validation, build, migration, replacement, and post-deploy checks.
main() {
  local env_file
  env_file="$(resolve_env_file)"

  cd "$ROOT_DIR"
  validate_env_file "$env_file"
  validate_runtime

  if [ "$VALIDATE_ONLY" = "1" ]; then
    log_step "Validation complete"
    echo "Env file: $env_file"
    return
  fi

  pull_latest
  build_image
  run_migrations "$env_file"
  replace_container "$env_file"
  wait_for_health
  cleanup_images
  print_summary
}

main "$@"

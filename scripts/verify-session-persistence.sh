#!/usr/bin/env bash
set -euo pipefail

# Sanitized closeout check for browser session persistence.
# Prints presence/status only. Never print JWT_SECRET, cookies, JWTs, CSRF
# tokens, PATs, or reversible fingerprints.

COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.selfhost.yml}"
PROJECT_NAME="${COMPOSE_PROJECT_NAME:-multica}"
BACKEND_SERVICE="${BACKEND_SERVICE:-backend}"
CONTAINER_NAMES=(
  "${PROJECT_NAME}-postgres-1"
  "${PROJECT_NAME}-backend-1"
  "${PROJECT_NAME}-frontend-1"
)

failures=0

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "FAIL: required command not found: $1"
    exit 1
  fi
}

record_fail() {
  echo "FAIL: $*"
  failures=$((failures + 1))
}

record_pass() {
  echo "PASS: $*"
}

require_command docker

if [ -f "$COMPOSE_FILE" ]; then
  if docker compose -f "$COMPOSE_FILE" config >/dev/null; then
    record_pass "compose config renders"
  else
    record_fail "compose config does not render"
  fi
fi

if docker compose -f "$COMPOSE_FILE" config 2>/dev/null \
  | awk -v svc="${BACKEND_SERVICE}:" '
      $1 == svc { in_service=1; next }
      in_service && $1 ~ /^[a-zA-Z0-9_-]+:$/ { in_service=0 }
      in_service && $1 == "JWT_SECRET:" {
        if ($0 ~ /change-me-in-production/ || $0 ~ /multica-dev-secret-change-in-production/) bad=1
        found=1
      }
      END { exit !(found && !bad) }
    '; then
  record_pass "backend JWT_SECRET is configured and not a known placeholder"
else
  record_fail "backend JWT_SECRET is missing or placeholder in compose config"
fi

for container in "${CONTAINER_NAMES[@]}"; do
  if ! docker inspect "$container" >/dev/null 2>&1; then
    record_fail "$container is not inspectable"
    continue
  fi

  started_at="$(docker inspect -f '{{.State.StartedAt}}' "$container")"
  restart_count="$(docker inspect -f '{{.RestartCount}}' "$container")"
  nano_cpus="$(docker inspect -f '{{.HostConfig.NanoCpus}}' "$container")"
  memory="$(docker inspect -f '{{.HostConfig.Memory}}' "$container")"
  memory_swap="$(docker inspect -f '{{.HostConfig.MemorySwap}}' "$container")"
  pids_limit="$(docker inspect -f '{{.HostConfig.PidsLimit}}' "$container")"

  echo "INFO: ${container} started_at=${started_at} restart_count=${restart_count}"

  if [ "${nano_cpus}" = "0" ]; then
    record_fail "$container has no CPU ceiling"
  else
    record_pass "$container CPU ceiling is set"
  fi
  if [ "${memory}" = "0" ]; then
    record_fail "$container has no memory ceiling"
  else
    record_pass "$container memory ceiling is set"
  fi
  if [ "${memory_swap}" = "0" ] || [ "${memory_swap}" = "-1" ]; then
    record_fail "$container has no finite swap ceiling"
  else
    record_pass "$container swap ceiling is set"
  fi
  if [ "${pids_limit}" = "0" ] || [ "${pids_limit}" = "-1" ]; then
    record_fail "$container has no PID ceiling"
  else
    record_pass "$container PID ceiling is set"
  fi
done

if docker inspect "${PROJECT_NAME}-backend-1" >/dev/null 2>&1; then
  warnings="$(docker logs --since 30m "${PROJECT_NAME}-backend-1" 2>&1 \
    | grep -E 'auth: invalid token|auth: no token found|CSRF validation failed' \
    | wc -l | tr -d ' ')"
  echo "INFO: backend auth-warning count since 30m=${warnings}"
fi

if [ "$failures" -gt 0 ]; then
  echo "FAIL: session persistence closeout check found ${failures} blocker(s)"
  exit 1
fi

echo "PASS: session persistence closeout check passed"

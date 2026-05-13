#!/usr/bin/env bash
set -euo pipefail

# Resource-safe Go test wrapper for shared/self-hosted Multica servers.
# Runs Go tests inside an isolated, resource-limited Docker container by default.
# Usage examples:
#   scripts/go-test-safe.sh ./internal/handler -run TestCRM -v
#   scripts/go-test-safe.sh ./...
# Optional:
#   GO_TEST_MODE=host scripts/go-test-safe.sh ./pkg/redact -run Test -v

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_DIR="$ROOT/server"

export GOMAXPROCS="${GOMAXPROCS:-1}"
export GOFLAGS="${GOFLAGS:--p=1}"
export REDIS_TEST_URL="${REDIS_TEST_URL:-}"

DEFAULT_TIMEOUT="${GO_TEST_TIMEOUT:-90s}"
DEFAULT_PARALLEL="${GO_TEST_PARALLEL:-1}"
GO_TEST_MODE="${GO_TEST_MODE:-docker}"
GO_TEST_IMAGE="${GO_TEST_IMAGE:-golang:1.26-alpine}"
GO_TEST_CPUS="${GO_TEST_CPUS:-1}"
GO_TEST_MEMORY="${GO_TEST_MEMORY:-2g}"
GO_TEST_PIDS_LIMIT="${GO_TEST_PIDS_LIMIT:-256}"
GO_TEST_NETWORK="${GO_TEST_NETWORK:-multica_default}"
GO_TEST_CACHE_ROOT="${GO_TEST_CACHE_ROOT:-$ROOT/.cache/go-test-safe}"

if [ "$#" -eq 0 ]; then
  set -- ./...
fi

has_timeout=false
has_parallel=false
has_count=false
for arg in "$@"; do
  case "$arg" in
    -timeout|-timeout=*) has_timeout=true ;;
    -parallel|-parallel=*) has_parallel=true ;;
    -count|-count=*) has_count=true ;;
  esac
done

extra_args=()
if [ "$has_timeout" = false ]; then
  extra_args+=("-timeout=${DEFAULT_TIMEOUT}")
fi
if [ "$has_parallel" = false ]; then
  extra_args+=("-parallel=${DEFAULT_PARALLEL}")
fi
if [ "$has_count" = false ]; then
  extra_args+=("-count=1")
fi

if [ "$GO_TEST_MODE" = "host" ]; then
  cd "$SERVER_DIR"
  echo "go-test-safe(host): GOMAXPROCS=$GOMAXPROCS GOFLAGS=$GOFLAGS REDIS_TEST_URL=${REDIS_TEST_URL:+set} timeout=${DEFAULT_TIMEOUT} parallel=${DEFAULT_PARALLEL}" >&2
  exec go test "$@" "${extra_args[@]}"
fi

mkdir -p "$GO_TEST_CACHE_ROOT/mod" "$GO_TEST_CACHE_ROOT/build"

docker_args=(
  run --rm
  --cpus "$GO_TEST_CPUS"
  --memory "$GO_TEST_MEMORY"
  --memory-swap "$GO_TEST_MEMORY"
  --pids-limit "$GO_TEST_PIDS_LIMIT"
  --network "$GO_TEST_NETWORK"
  -e GOMAXPROCS="$GOMAXPROCS"
  -e GOFLAGS="$GOFLAGS"
  -e GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
  -e REDIS_TEST_URL="$REDIS_TEST_URL"
  -e CGO_ENABLED="${CGO_ENABLED:-0}"
  -e GOMODCACHE=/go/pkg/mod
  -e GOCACHE=/root/.cache/go-build
  -v "$SERVER_DIR:/workspace:ro"
  -v "$GO_TEST_CACHE_ROOT/mod:/go/pkg/mod"
  -v "$GO_TEST_CACHE_ROOT/build:/root/.cache/go-build"
  -w /workspace
)

if [ -f "$ROOT/.env" ]; then
  docker_args+=(--env-file "$ROOT/.env")
fi

# If .env targets the host-published Postgres port, override with the Compose service DNS
# inside the multica_default network. Password/user/db are still read from .env.
docker_args+=(
  -e POSTGRES_HOST="${POSTGRES_HOST:-postgres}"
  -e POSTGRES_PORT="${POSTGRES_CONTAINER_PORT:-5432}"
)

echo "go-test-safe(docker): image=$GO_TEST_IMAGE cpus=$GO_TEST_CPUS memory=$GO_TEST_MEMORY pids=$GO_TEST_PIDS_LIMIT network=$GO_TEST_NETWORK GOMAXPROCS=$GOMAXPROCS GOFLAGS=$GOFLAGS REDIS_TEST_URL=${REDIS_TEST_URL:+set} timeout=${DEFAULT_TIMEOUT} parallel=${DEFAULT_PARALLEL}" >&2
exec docker "${docker_args[@]}" "$GO_TEST_IMAGE" go test "$@" "${extra_args[@]}"

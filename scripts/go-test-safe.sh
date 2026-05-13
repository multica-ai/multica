#!/usr/bin/env bash
set -euo pipefail

# Resource-safe Go test wrapper for shared/self-hosted Multica servers.
# Defaults are intentionally conservative to avoid saturating 4c/8G hosts.
# Usage examples:
#   scripts/go-test-safe.sh ./internal/handler -run TestCRM -v
#   scripts/go-test-safe.sh ./...

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT/server"

export GOMAXPROCS="${GOMAXPROCS:-1}"
export GOFLAGS="${GOFLAGS:--p=1}"
export REDIS_TEST_URL="${REDIS_TEST_URL:-}"

DEFAULT_TIMEOUT="${GO_TEST_TIMEOUT:-90s}"
DEFAULT_PARALLEL="${GO_TEST_PARALLEL:-1}"

if [ "$#" -eq 0 ]; then
  set -- ./...
fi

has_timeout=false
has_parallel=false
for arg in "$@"; do
  case "$arg" in
    -timeout|-timeout=*) has_timeout=true ;;
    -parallel|-parallel=*) has_parallel=true ;;
  esac
done

extra_args=()
if [ "$has_timeout" = false ]; then
  extra_args+=("-timeout=${DEFAULT_TIMEOUT}")
fi
if [ "$has_parallel" = false ]; then
  extra_args+=("-parallel=${DEFAULT_PARALLEL}")
fi

echo "go-test-safe: GOMAXPROCS=$GOMAXPROCS GOFLAGS=$GOFLAGS REDIS_TEST_URL=${REDIS_TEST_URL:+set} timeout=${DEFAULT_TIMEOUT} parallel=${DEFAULT_PARALLEL}" >&2
exec go test "$@" "${extra_args[@]}" -count=1

#!/usr/bin/env bash
set -euo pipefail

# CEREBRO-PATCH(check-frontend-readiness): regression coverage for FIR-3266.
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

# shellcheck disable=SC1091
. scripts/frontend-readiness.sh

curl() {
  if [ "${*: -1}" != "http://localhost:3000/login" ]; then
    echo "Readiness must probe /login; got: ${*: -1}" >&2
    return 2
  fi
  printf '%s' "${MOCK_FRONTEND_BODY:-}"
}

MOCK_FRONTEND_BODY='<h1>Page not found</h1>'
if frontend_login_ready "http://localhost:3000"; then
  echo "A 404 page must not satisfy frontend readiness."
  exit 1
fi

MOCK_FRONTEND_BODY='<h1>Sign in to Multica</h1>'
if ! frontend_login_ready "http://localhost:3000"; then
  echo "The rendered login page should satisfy frontend readiness."
  exit 1
fi

echo "frontend readiness checks ok"

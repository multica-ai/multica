#!/usr/bin/env bash
set -euo pipefail

unset CEREBRO_MINI_APPS_ENABLED

# shellcheck disable=SC1091
. scripts/check-e2e-env.sh

if [ "${CEREBRO_MINI_APPS_ENABLED:-}" != "true" ]; then
  echo "expected E2E checks to enable the mini-app server surface"
  exit 1
fi

echo "check E2E environment tests passed"

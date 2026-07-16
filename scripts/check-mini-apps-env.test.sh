#!/usr/bin/env bash
set -euo pipefail

# shellcheck disable=SC1091
. scripts/check-mini-apps-env.sh

unset CEREBRO_MINI_APPS_ENABLED CEREBRO_APPS_RUNTIME_URL CEREBRO_APPS_RUNTIME_SERVICE_KEY
configure_check_mini_apps_env
test "$CEREBRO_MINI_APPS_ENABLED" = "true"
test "$CEREBRO_APPS_RUNTIME_URL" = "http://127.0.0.1:4310"
test "$CEREBRO_APPS_RUNTIME_SERVICE_KEY" = "local-check-mini-apps-key"

CEREBRO_MINI_APPS_ENABLED=false
CEREBRO_APPS_RUNTIME_URL=http://127.0.0.1:9876
CEREBRO_APPS_RUNTIME_SERVICE_KEY=caller-provided-key
configure_check_mini_apps_env
test "$CEREBRO_MINI_APPS_ENABLED" = "false"
test "$CEREBRO_APPS_RUNTIME_URL" = "http://127.0.0.1:9876"
test "$CEREBRO_APPS_RUNTIME_SERVICE_KEY" = "caller-provided-key"

echo "check mini-apps env tests: PASS"

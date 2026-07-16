#!/usr/bin/env bash
set -euo pipefail

# shellcheck disable=SC1091
. scripts/check-mini-apps-env.sh

unset CEREBRO_MINI_APPS_ENABLED
configure_check_mini_apps_env
test "$CEREBRO_MINI_APPS_ENABLED" = "true"

CEREBRO_MINI_APPS_ENABLED=false
configure_check_mini_apps_env
test "$CEREBRO_MINI_APPS_ENABLED" = "false"

echo "check mini-apps env tests: PASS"

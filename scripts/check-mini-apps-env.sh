#!/usr/bin/env bash

# Mini-app HTTP routes have a server-level kill switch in addition to the
# workspace feature flag. The E2E fixture controls the workspace flag itself,
# so the local check stack must expose the routes unless the caller explicitly
# disabled them.
configure_check_mini_apps_env() {
  : "${CEREBRO_MINI_APPS_ENABLED:=true}"
  export CEREBRO_MINI_APPS_ENABLED
}

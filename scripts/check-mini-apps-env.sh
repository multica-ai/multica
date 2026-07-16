#!/usr/bin/env bash

# Mini-app HTTP routes have a server-level kill switch in addition to the
# workspace feature flag. The E2E fixture controls the workspace flag itself,
# so the local check stack must expose the routes unless the caller explicitly
# disabled them.
configure_check_mini_apps_env() {
  : "${CEREBRO_MINI_APPS_ENABLED:=true}"
  : "${CEREBRO_APPS_RUNTIME_URL:=http://127.0.0.1:4310}"
  : "${CEREBRO_APPS_RUNTIME_SERVICE_KEY:=local-check-mini-apps-key}"
  export CEREBRO_MINI_APPS_ENABLED
  export CEREBRO_APPS_RUNTIME_URL
  export CEREBRO_APPS_RUNTIME_SERVICE_KEY
}

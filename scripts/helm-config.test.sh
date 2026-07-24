#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHART_DIR="$ROOT_DIR/deploy/helm/multica"

require_rendered_value() {
  local rendered=$1
  local expected=$2

  if ! grep -Fq "$expected" <<<"$rendered"; then
    echo "Missing expected Helm-rendered config value:"
    echo "  $expected"
    exit 1
  fi
}

first_rendered_service() {
  local rendered=$1

  awk '
    /^kind: Service$/ {
      found = 1
    }
    found {
      print
    }
    found && /^---$/ {
      exit
    }
  ' <<<"$rendered"
}

helm lint "$CHART_DIR"

default_config="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/configmap.yaml
)"
require_rendered_value "$default_config" 'MULTICA_VCS_INTEGRATION_ENABLED: "true"'

disabled_config="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/configmap.yaml \
    --set backend.config.vcsIntegrationEnabled=false
)"
require_rendered_value "$disabled_config" 'MULTICA_VCS_INTEGRATION_ENABLED: "false"'

backend_service="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/backend.yaml \
    --set-json 'backend.service.labels={"team":"platform"}' \
    --set-json 'backend.service.annotations={"example.com/backend":"true"}'
)"
backend_service="$(first_rendered_service "$backend_service")"
require_rendered_value "$backend_service" 'team: platform'
require_rendered_value "$backend_service" 'example.com/backend: "true"'

frontend_service="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/frontend.yaml \
    --set-json 'frontend.service.labels={"team":"web"}' \
    --set-json 'frontend.service.annotations={"example.com/frontend":"true"}'
)"
frontend_service="$(first_rendered_service "$frontend_service")"
require_rendered_value "$frontend_service" 'team: web'
require_rendered_value "$frontend_service" 'example.com/frontend: "true"'

echo "helm config rendering ok"

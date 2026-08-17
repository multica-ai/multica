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

helm lint "$CHART_DIR"

default_config="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/configmap.yaml
)"
require_rendered_value "$default_config" 'MULTICA_VCS_INTEGRATION_ENABLED: "true"'
require_rendered_value "$default_config" 'NTFY_ENABLED: "false"'
require_rendered_value "$default_config" 'NTFY_BASE_URL: "https://ntfy.sh"'
require_rendered_value "$default_config" 'NTFY_TIMEOUT: "3s"'

disabled_config="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/configmap.yaml \
    --set backend.config.vcsIntegrationEnabled=false
)"
require_rendered_value "$disabled_config" 'MULTICA_VCS_INTEGRATION_ENABLED: "false"'

ntfy_config="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/configmap.yaml \
    --set backend.config.ntfyEnabled=true \
    --set backend.config.ntfyBaseUrl=https://ntfy.example \
    --set backend.config.ntfyTimeout=750ms
)"
require_rendered_value "$ntfy_config" 'NTFY_ENABLED: "true"'
require_rendered_value "$ntfy_config" 'NTFY_BASE_URL: "https://ntfy.example"'
require_rendered_value "$ntfy_config" 'NTFY_TIMEOUT: "750ms"'

echo "helm config rendering ok"

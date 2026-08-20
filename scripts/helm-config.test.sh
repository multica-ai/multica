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

reject_rendered_value() {
  local rendered=$1
  local forbidden=$2

  if grep -Fq "$forbidden" <<<"$rendered"; then
    echo "Forbidden Helm-rendered config value:"
    echo "  $forbidden"
    exit 1
  fi
}

helm lint "$CHART_DIR"

default_config="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/configmap.yaml
)"
require_rendered_value "$default_config" 'MULTICA_VCS_INTEGRATION_ENABLED: "true"'
require_rendered_value "$default_config" 'MULTICA_ENTITLEMENT_POLICY_ENABLED: "false"'
require_rendered_value "$default_config" 'MULTICA_ENTITLEMENT_POLICY_URL: ""'
reject_rendered_value "$default_config" 'MULTICA_ENTITLEMENT_SERVICE_TOKEN'

disabled_config="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/configmap.yaml \
    --set backend.config.vcsIntegrationEnabled=false
)"
require_rendered_value "$disabled_config" 'MULTICA_VCS_INTEGRATION_ENABLED: "false"'

entitlement_config="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/configmap.yaml \
    --set backend.config.entitlementPolicy.enabled=true \
    --set-string backend.config.entitlementPolicy.url=https://multica-cloud.internal \
    --set-string backend.config.entitlementPolicy.timeout=2s \
    --set-string backend.config.entitlementPolicy.staleGrace=10m \
    --set backend.config.entitlementPolicy.emergencyDisabled=false
)"
require_rendered_value "$entitlement_config" 'MULTICA_ENTITLEMENT_POLICY_ENABLED: "true"'
require_rendered_value "$entitlement_config" 'MULTICA_ENTITLEMENT_POLICY_URL: "https://multica-cloud.internal"'
require_rendered_value "$entitlement_config" 'MULTICA_ENTITLEMENT_POLICY_TIMEOUT: "2s"'
require_rendered_value "$entitlement_config" 'MULTICA_ENTITLEMENT_STALE_GRACE: "10m"'
require_rendered_value "$entitlement_config" 'MULTICA_ENTITLEMENT_EMERGENCY_DISABLED: "false"'
reject_rendered_value "$entitlement_config" 'MULTICA_ENTITLEMENT_SERVICE_TOKEN'

echo "helm config rendering ok"

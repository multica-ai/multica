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
require_rendered_value "$default_config" 'MULTICA_CLOUD_URL: ""'
require_rendered_value "$default_config" 'MULTICA_DATABASE_STARTUP_TIMEOUT: "3m"'
require_rendered_value "$default_config" 'MULTICA_DATABASE_CONNECT_TIMEOUT: "5s"'

# Feature flags are part of the self-host contract: Workflows are GA and on by
# default, and operators can flip any flag through values without editing
# templates. A chart that silently drops these keys leaves the shipped default
# in place, so assert the rendered env entries themselves.
require_rendered_value "$default_config" 'MULTICA_FEATURE_FLAGS_FILE: ""'
require_rendered_value "$default_config" 'FF_PROJECT_WORKFLOWS_V1: "true"'
workflows_disabled_config="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/configmap.yaml \
    --set backend.config.featureFlags.project_workflows_v1=false
)"
require_rendered_value "$workflows_disabled_config" 'FF_PROJECT_WORKFLOWS_V1: "false"'
plugins_enabled_config="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/configmap.yaml \
    --set backend.config.featureFlags.plugins_v1=true
)"
require_rendered_value "$plugins_enabled_config" 'FF_PLUGINS_V1: "true"'
flags_file_config="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/configmap.yaml \
    --set backend.config.featureFlagsFile=/etc/multica/feature-flags.yaml
)"
require_rendered_value "$flags_file_config" 'MULTICA_FEATURE_FLAGS_FILE: "/etc/multica/feature-flags.yaml"'
# `helm upgrade --reuse-values` from a release that predates featureFlags
# carries no map at all; the chart must still keep Workflows on.
reuse_values_config="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/configmap.yaml \
    --set backend.config.featureFlags=null
)"
require_rendered_value "$reuse_values_config" 'FF_PROJECT_WORKFLOWS_V1: "true"'

require_rendered_value "$default_config" 'MAINTENANCE_PORT: ""'
maintenance_config="$(helm template multica "$CHART_DIR" --show-only templates/configmap.yaml --set-string backend.config.maintenancePort=6061)"
require_rendered_value "$maintenance_config" 'MAINTENANCE_PORT: "6061"'
maintenance_backend="$(helm template multica "$CHART_DIR" --show-only templates/backend.yaml --set-string backend.config.maintenancePort=6061)"
reject_rendered_value "$maintenance_backend" 'containerPort: 6061'
reject_rendered_value "$maintenance_backend" 'port: 6061'

default_backend="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/backend.yaml
)"
require_rendered_value "$default_backend" 'failureThreshold: 60'
liveness_block="$(sed -n '/livenessProbe:/,/resources:/p' <<<"$default_backend")"
require_rendered_value "$liveness_block" 'path: /health'
reject_rendered_value "$liveness_block" 'path: /healthz'
reject_rendered_value "$default_backend" 'SSL_CERT_DIR'
reject_rendered_value "$default_backend" 'extra-ca-certs'

# `helm upgrade --reuse-values` from a chart that predates extraCACerts renders
# without the key at all; that must still render, with no extra trust.
legacy_backend="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/backend.yaml \
    --set backend.extraCACerts=null
)"
reject_rendered_value "$legacy_backend" 'SSL_CERT_DIR'

extra_ca_backend="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/backend.yaml \
    --set backend.extraCACerts.configMap=internal-ca
)"
require_rendered_value "$extra_ca_backend" 'value: /etc/ssl/certs:/etc/multica/ca-certs'
require_rendered_value "$extra_ca_backend" 'mountPath: /etc/multica/ca-certs'
require_rendered_value "$extra_ca_backend" 'name: internal-ca'
require_rendered_value "$extra_ca_backend" 'mountPath: /app/data/uploads'

disabled_config="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/configmap.yaml \
    --set backend.config.vcsIntegrationEnabled=false
)"
require_rendered_value "$disabled_config" 'MULTICA_VCS_INTEGRATION_ENABLED: "false"'

capacity_config="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/configmap.yaml \
    --set-string backend.config.cloud.url=https://multica-cloud.internal
)"
require_rendered_value "$capacity_config" 'MULTICA_CLOUD_URL: "https://multica-cloud.internal"'

echo "helm config rendering ok"

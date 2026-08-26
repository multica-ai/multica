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
require_rendered_value "$default_config" 'MULTICA_TASK_QUEUED_TTL: "2h"'
require_rendered_value "$default_config" 'MULTICA_DATABASE_STARTUP_TIMEOUT: "3m"'
require_rendered_value "$default_config" 'MULTICA_DATABASE_CONNECT_TIMEOUT: "5s"'

queued_ttl_config="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/configmap.yaml \
    --set-string backend.config.taskQueuedTTL=12h
)"
require_rendered_value "$queued_ttl_config" 'MULTICA_TASK_QUEUED_TTL: "12h"'

default_backend="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/backend.yaml
)"
require_rendered_value "$default_backend" 'failureThreshold: 60'
liveness_block="$(sed -n '/livenessProbe:/,/resources:/p' <<<"$default_backend")"
require_rendered_value "$liveness_block" 'path: /health'
reject_rendered_value "$liveness_block" 'path: /healthz'

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

capacity_alerts="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/prometheusrule.yaml \
    --set monitoring.prometheusRule.enabled=true
)"
require_rendered_value "$capacity_alerts" 'alert: MulticaSeatCapacityOutboxDeadLettered'
require_rendered_value "$capacity_alerts" 'alert: MulticaSeatCapacityOutboxStalled'
require_rendered_value "$capacity_alerts" 'multica_seat_capacity_outbox_oldest_pending_age_seconds'

echo "helm config rendering ok"

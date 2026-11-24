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
require_rendered_value "$default_config" 'MULTICA_MAX_PREVIEW_SIZE: "2MiB"'
preview_config="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/configmap.yaml \
    --set-string backend.config.maxPreviewSize=8MiB
)"
require_rendered_value "$preview_config" 'MULTICA_MAX_PREVIEW_SIZE: "8MiB"'

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

# Live co-editing is off by default and then renders nothing: no collab
# workload, and no backend setting that would point the API at one.
default_all="$(helm template multica "$CHART_DIR")"
reject_rendered_value "$default_all" 'multica-collab'
reject_rendered_value "$default_all" 'COLLAB_'

collab_all="$(helm template multica "$CHART_DIR" --set collab.enabled=true)"
require_rendered_value "$collab_all" 'name: multica-collab'
require_rendered_value "$collab_all" 'image: "ghcr.io/multica-ai/multica-collab:'
require_rendered_value "$collab_all" 'value: "http://multica-backend:8080"'
collab_deployment="$(helm template multica "$CHART_DIR" --set collab.enabled=true --show-only templates/collab.yaml | sed -n '/kind: Deployment/,$p')"
require_rendered_value "$collab_deployment" 'replicas: 1'
require_rendered_value "$collab_deployment" 'type: Recreate'
require_rendered_value "$collab_deployment" 'terminationGracePeriodSeconds: 30'
require_rendered_value "$collab_deployment" 'path: /health'
require_rendered_value "$collab_deployment" 'key: COLLAB_SECRET'
# The backend reports the host of LOCAL_UPLOAD_BASE_URL as its CDN domain
# without S3, and CLOUDFRONT_DOMAIN with it; collab must agree.
require_rendered_value "$collab_deployment" 'value: "api.multica.dev.lan"'
collab_s3="$(helm template multica "$CHART_DIR" --set collab.enabled=true \
  --set backend.config.s3Bucket=bucket --set backend.config.cloudfrontDomain=cdn.example.com \
  --show-only templates/collab.yaml)"
require_rendered_value "$collab_s3" 'value: "cdn.example.com"'

collab_backend="$(helm template multica "$CHART_DIR" --set collab.enabled=true --show-only templates/backend.yaml)"
require_rendered_value "$collab_backend" 'value: "http://multica-collab:8095"'
require_rendered_value "$collab_backend" 'key: COLLAB_SECRET'
require_rendered_value "$collab_backend" 'optional: true'
# External postgres and no extra CAs leave the backend env block otherwise empty.
collab_backend_external="$(helm template multica "$CHART_DIR" --set collab.enabled=true \
  --set postgres.external.enabled=true --show-only templates/backend.yaml)"
require_rendered_value "$collab_backend_external" 'name: COLLAB_INTERNAL_URL'
reject_rendered_value "$collab_backend_external" 'name: DATABASE_URL'

# Feature flags render nothing by default, so every flag keeps the default the
# server gives it. Listed flags land in the ConfigMap the backend loads with
# envFrom, under the FF_<KEY> name the server looks up.
reject_rendered_value "$default_all" 'FF_'
flags_config="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/configmap.yaml \
    --set backend.config.featureFlags.docs_v1=true \
    --set backend.config.featureFlags.local_search_index=false \
    --set-string 'backend.config.featureFlags.checkout\.algo-v2=25%'
)"
require_rendered_value "$flags_config" 'FF_DOCS_V1: "true"'
require_rendered_value "$flags_config" 'FF_LOCAL_SEARCH_INDEX: "false"'
require_rendered_value "$flags_config" 'FF_CHECKOUT_ALGO_V2: "25%"'
# `helm upgrade --reuse-values` from a chart without featureFlags.
legacy_flags_config="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/configmap.yaml \
    --set backend.config.featureFlags=null
)"
reject_rendered_value "$legacy_flags_config" 'FF_'

capacity_config="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/configmap.yaml \
    --set-string backend.config.cloud.url=https://multica-cloud.internal
)"
require_rendered_value "$capacity_config" 'MULTICA_CLOUD_URL: "https://multica-cloud.internal"'

echo "helm config rendering ok"

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

capacity_config="$(
  helm template multica "$CHART_DIR" \
    --show-only templates/configmap.yaml \
    --set-string backend.config.cloud.url=https://multica-cloud.internal
)"
require_rendered_value "$capacity_config" 'MULTICA_CLOUD_URL: "https://multica-cloud.internal"'

# Image digests. A tag-only install must render exactly as before: no digest
# means repository:tag (appVersion for backend/frontend when tag is empty).
backend_digest="sha256:$(printf 'a%.0s' {1..64})"
postgres_digest="sha256:$(printf 'b%.0s' {1..64})"
chart_app_version="$(sed -n 's/^appVersion:[[:space:]]*"\{0,1\}\([^"]*\)"\{0,1\}$/\1/p' "$CHART_DIR/Chart.yaml")"

default_images="$(helm template multica "$CHART_DIR" | grep 'image:')"
require_rendered_value "$default_images" "image: \"ghcr.io/multica-ai/multica-backend:$chart_app_version\""
require_rendered_value "$default_images" "image: \"ghcr.io/multica-ai/multica-web:$chart_app_version\""
require_rendered_value "$default_images" 'image: "pgvector/pgvector:pg17"'
reject_rendered_value "$default_images" '@sha256:'

tagged_images="$(
  helm template multica "$CHART_DIR" \
    --set-string images.backend.tag=v1.2.3 \
    --set-string images.frontend.tag=v1.2.4 \
    --set-string images.postgres.tag=pg16 | grep 'image:'
)"
require_rendered_value "$tagged_images" 'image: "ghcr.io/multica-ai/multica-backend:v1.2.3"'
require_rendered_value "$tagged_images" 'image: "ghcr.io/multica-ai/multica-web:v1.2.4"'
require_rendered_value "$tagged_images" 'image: "pgvector/pgvector:pg16"'

# Mixed: pinned images render repository@digest (the digest wins over a tag),
# unpinned images keep repository:tag.
mixed_images="$(
  helm template multica "$CHART_DIR" \
    --set-string images.backend.tag=v1.2.3 \
    --set-string images.backend.digest="$backend_digest" \
    --set-string images.frontend.tag=v1.2.4 \
    --set-string images.postgres.digest="$postgres_digest" | grep 'image:'
)"
require_rendered_value "$mixed_images" "image: \"ghcr.io/multica-ai/multica-backend@$backend_digest\""
require_rendered_value "$mixed_images" 'image: "ghcr.io/multica-ai/multica-web:v1.2.4"'
require_rendered_value "$mixed_images" "image: \"pgvector/pgvector@$postgres_digest\""
reject_rendered_value "$mixed_images" 'multica-backend:v1.2.3'

# `helm upgrade --reuse-values` from a chart that predates digests renders
# without the key at all.
legacy_images="$(helm template multica "$CHART_DIR" --set images.backend.digest=null | grep 'image:')"
require_rendered_value "$legacy_images" "image: \"ghcr.io/multica-ai/multica-backend:$chart_app_version\""

for invalid_digest in "sha256:abc" "${backend_digest}0" "sha256:$(printf 'A%.0s' {1..64})" "sha512:$(printf 'a%.0s' {1..128})" "v1.2.3"; do
  if invalid_output="$(helm template multica "$CHART_DIR" --set-string images.frontend.digest="$invalid_digest" 2>&1)"; then
    echo "Expected Helm to reject images.frontend.digest=$invalid_digest"
    exit 1
  fi
  require_rendered_value "$invalid_output" 'images.frontend.digest must be a sha256 digest'
done

echo "helm config rendering ok"

#!/usr/bin/env bash

# Helm chart render tests (no Kubernetes cluster required).
# Verifies the default Ingress resources, Gateway API HTTPRoutes and their
# configured parent references/hostnames/backend ports, the no-exposure mode,
# and rejection of simultaneously enabled Ingress and Gateway API modes.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHART_DIR="$ROOT_DIR/deploy/helm/multica"

fail() {
  echo "helm chart test failed: $*" >&2
  exit 1
}

count_kind() {
  local manifest=$1
  local kind=$2
  awk -v kind="$kind" '$1 == "kind:" && $2 == kind { count++ } END { print count + 0 }' <<<"$manifest"
}

require_count() {
  local manifest=$1
  local kind=$2
  local expected=$3
  local actual
  actual="$(count_kind "$manifest" "$kind")"
  [[ "$actual" == "$expected" ]] || fail "expected $expected $kind resources, rendered $actual"
}

require_occurrences() {
  local content=$1
  local expected=$2
  local needle=$3
  local actual
  actual="$(grep -Fxc "$needle" <<<"$content" || true)"
  [[ "$actual" == "$expected" ]] || fail "expected '$needle' $expected time(s), rendered $actual"
}

routes_only() {
  awk 'BEGIN { RS = "---" } /kind: HTTPRoute/ { print }'
}

ingress_manifest="$(helm template multica "$CHART_DIR" --namespace multica)"
require_count "$ingress_manifest" Ingress 2
require_count "$ingress_manifest" HTTPRoute 0

gateway_manifest="$(
  helm template multica "$CHART_DIR" \
    --namespace multica \
    --set ingress.enabled=false \
    --set gatewayAPI.enabled=true \
    --set 'gatewayAPI.parentRefs[0].name=shared-gateway' \
    --set 'gatewayAPI.parentRefs[0].namespace=gateway-system' \
    --set 'gatewayAPI.parentRefs[0].sectionName=https' \
    --set 'gatewayAPI.frontend.hostnames[0]=app.example.com' \
    --set 'gatewayAPI.backend.hostnames[0]=api.example.com'
)"
require_count "$gateway_manifest" Ingress 0
require_count "$gateway_manifest" HTTPRoute 2

gateway_routes="$(routes_only <<<"$gateway_manifest")"
require_occurrences "$gateway_routes" 2 '    - name: shared-gateway'
require_occurrences "$gateway_routes" 2 '      namespace: gateway-system'
require_occurrences "$gateway_routes" 2 '      sectionName: https'
require_occurrences "$gateway_routes" 1 '    - app.example.com'
require_occurrences "$gateway_routes" 1 '    - api.example.com'
require_occurrences "$gateway_routes" 1 '          port: 3000'
require_occurrences "$gateway_routes" 1 '          port: 8080'

no_exposure_manifest="$(
  helm template multica "$CHART_DIR" \
    --namespace multica \
    --set ingress.enabled=false \
    --set gatewayAPI.enabled=false
)"
require_count "$no_exposure_manifest" Ingress 0
require_count "$no_exposure_manifest" HTTPRoute 0

combined_output="$(mktemp)"
trap 'rm -f "$combined_output"' EXIT
if helm template multica "$CHART_DIR" \
  --namespace multica \
  --set ingress.enabled=true \
  --set gatewayAPI.enabled=true >"$combined_output" 2>&1; then
  fail "combined Ingress and Gateway API mode must be rejected"
fi
grep -Fq 'ingress.enabled and gatewayAPI.enabled are mutually exclusive' "$combined_output" ||
  fail "combined-mode rejection did not explain how to select one exposure mode"

echo "helm chart render tests passed"

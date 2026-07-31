#!/usr/bin/env bash

# Envoy Gateway integration tests against the active Kubernetes context.
# Creates temporary Gateway, TLS, and route fixtures to verify that the chart's
# frontend and backend HTTPRoutes attach to permitted cross-namespace HTTP/HTTPS
# listeners and are rejected by a same-namespace-only listener. Fixtures are
# removed when the script exits; Envoy Gateway and Gateway API CRDs must exist.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHART_DIR="$ROOT_DIR/deploy/helm/multica"
GATEWAY_NAMESPACE="multica-gateway-test"
ROUTE_NAMESPACE="multica-route-test"
GATEWAY_NAME="multica-test"

for command in helm kubectl openssl; do
  command -v "$command" >/dev/null || {
    echo "missing required command: $command" >&2
    exit 1
  }
done

cleanup() {
  kubectl delete namespace "$ROUTE_NAMESPACE" "$GATEWAY_NAMESPACE" --ignore-not-found --wait=true >/dev/null 2>&1 || true
  kubectl delete gatewayclass multica-envoy-test --ignore-not-found --wait=true >/dev/null 2>&1 || true
}
trap cleanup EXIT
cleanup

kubectl create namespace "$GATEWAY_NAMESPACE"
kubectl create namespace "$ROUTE_NAMESPACE"
kubectl label namespace "$ROUTE_NAMESPACE" multica.ai/gateway-access=allowed

cert_dir="$(mktemp -d)"
trap 'rm -rf "$cert_dir"; cleanup' EXIT
openssl req -x509 -nodes -newkey rsa:2048 \
  -keyout "$cert_dir/tls.key" \
  -out "$cert_dir/tls.crt" \
  -days 1 \
  -subj '/CN=multica.dev.lan' \
  -addext 'subjectAltName=DNS:multica.dev.lan,DNS:api.multica.dev.lan' >/dev/null 2>&1
kubectl -n "$GATEWAY_NAMESPACE" create secret tls multica-test-tls \
  --key "$cert_dir/tls.key" \
  --cert "$cert_dir/tls.crt"

kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: multica-envoy-test
spec:
  controllerName: gateway.envoyproxy.io/gatewayclass-controller
---
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: ${GATEWAY_NAME}
  namespace: ${GATEWAY_NAMESPACE}
spec:
  gatewayClassName: multica-envoy-test
  listeners:
    - name: http
      protocol: HTTP
      port: 80
      allowedRoutes:
        namespaces:
          from: Selector
          selector:
            matchLabels:
              multica.ai/gateway-access: allowed
        kinds:
          - kind: HTTPRoute
    - name: https
      protocol: HTTPS
      port: 443
      tls:
        mode: Terminate
        certificateRefs:
          - kind: Secret
            name: multica-test-tls
      allowedRoutes:
        namespaces:
          from: Selector
          selector:
            matchLabels:
              multica.ai/gateway-access: allowed
        kinds:
          - kind: HTTPRoute
    - name: blocked
      protocol: HTTP
      port: 8080
      allowedRoutes:
        namespaces:
          from: Same
        kinds:
          - kind: HTTPRoute
EOF

kubectl -n "$GATEWAY_NAMESPACE" wait \
  --for=condition=Accepted \
  "gateway/$GATEWAY_NAME" \
  --timeout=180s

wait_for_listener_condition() {
  local listener=$1
  local expected=$2
  local observed

  for _ in $(seq 1 90); do
    observed="$(
      kubectl -n "$GATEWAY_NAMESPACE" get gateway "$GATEWAY_NAME" \
        -o jsonpath='{range .status.listeners[*]}{.name}{"|"}{range .conditions[*]}{.type}{"="}{.status}{":"}{.reason}{","}{end}{"\n"}{end}' \
        2>/dev/null || true
    )"
    while IFS= read -r listener_status; do
      if [[ "$listener_status" == "$listener|"* && "$listener_status" == *"$expected"* ]]; then
        return 0
      fi
    done <<<"$observed"
    sleep 2
  done

  echo "listener $listener did not report $expected" >&2
  echo "observed listeners:" >&2
  echo "$observed" >&2
  return 1
}

wait_for_listener_condition http 'Programmed=True:Programmed'
wait_for_listener_condition https 'ResolvedRefs=True:ResolvedRefs'
wait_for_listener_condition https 'Programmed=True:Programmed'

wait_for_route_parent() {
  local route=$1
  local section=$2
  local expected=$3
  local observed

  for _ in $(seq 1 90); do
    observed="$(
      kubectl -n "$ROUTE_NAMESPACE" get httproute "$route" \
        -o jsonpath='{range .status.parents[*]}{.parentRef.sectionName}{"|"}{range .conditions[*]}{.type}{"="}{.status}{":"}{.reason}{","}{end}{"\n"}{end}' \
        2>/dev/null || true
    )"
    while IFS= read -r parent_status; do
      if [[ "$parent_status" == "$section|"* && "$parent_status" == *"$expected"* ]]; then
        return 0
      fi
    done <<<"$observed"
    sleep 2
  done

  echo "route $route did not report $section|$expected" >&2
  echo "observed route parents:" >&2
  echo "$observed" >&2
  return 1
}

helm template multica "$CHART_DIR" \
  --namespace "$ROUTE_NAMESPACE" \
  --show-only templates/httproute.yaml \
  --set ingress.enabled=false \
  --set gatewayAPI.enabled=true \
  --set "gatewayAPI.parentRefs[0].name=$GATEWAY_NAME" \
  --set "gatewayAPI.parentRefs[0].namespace=$GATEWAY_NAMESPACE" \
  --set 'gatewayAPI.parentRefs[0].sectionName=http' \
  --set "gatewayAPI.parentRefs[1].name=$GATEWAY_NAME" \
  --set "gatewayAPI.parentRefs[1].namespace=$GATEWAY_NAMESPACE" \
  --set 'gatewayAPI.parentRefs[1].sectionName=https' |
  kubectl -n "$ROUTE_NAMESPACE" apply -f -

for route in multica-frontend multica-backend; do
  wait_for_route_parent "$route" http 'Accepted=True:Accepted'
  wait_for_route_parent "$route" https 'Accepted=True:Accepted'
done

helm template blocked "$CHART_DIR" \
  --namespace "$ROUTE_NAMESPACE" \
  --show-only templates/httproute.yaml \
  --set ingress.enabled=false \
  --set gatewayAPI.enabled=true \
  --set "gatewayAPI.parentRefs[0].name=$GATEWAY_NAME" \
  --set "gatewayAPI.parentRefs[0].namespace=$GATEWAY_NAMESPACE" \
  --set 'gatewayAPI.parentRefs[0].sectionName=blocked' |
  kubectl -n "$ROUTE_NAMESPACE" apply -f -

for route in blocked-frontend blocked-backend; do
  wait_for_route_parent "$route" blocked 'Accepted=False:NotAllowedByListeners'
done

echo "Envoy Gateway attachment integration tests passed"

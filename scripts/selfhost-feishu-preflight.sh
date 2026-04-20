#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${1:-${ENV_FILE:-.env}}"

if [ ! -f "$ENV_FILE" ]; then
  echo "Missing env file: $ENV_FILE"
  echo "Create .env from .env.example first."
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

missing=0
for key in FEISHU_APP_ID FEISHU_APP_SECRET FEISHU_REDIRECT_URI NEXT_PUBLIC_FEISHU_APP_ID; do
  if [ -z "${!key:-}" ]; then
    echo "MISSING: $key"
    missing=1
  else
    echo "OK: $key"
  fi
done

backend_url="http://127.0.0.1:${PORT:-13080}"
frontend_url="http://127.0.0.1:${FRONTEND_PORT:-13030}"
redirect_uri="${FEISHU_REDIRECT_URI:-${frontend_url}/auth/callback}"

if curl -fsS "$backend_url/health" >/dev/null 2>&1; then
  echo "OK: backend health endpoint"
else
  echo "WARN: backend health endpoint is unavailable"
fi

if html="$(curl -fsS "$frontend_url/login" 2>/dev/null)"; then
  if printf '%s' "$html" | grep -q 'Continue with Feishu'; then
    echo "OK: login page shows Feishu button"
  elif grep -R -q 'Continue with Feishu' ./apps/web/.next 2>/dev/null; then
    echo "OK: frontend bundle includes Feishu login UI"
    echo "NOTE: the login page is client-rendered, so raw curl HTML may not contain the button text"
  else
    echo "WARN: login page does not show Feishu button"
    echo "HINT: if NEXT_PUBLIC_FEISHU_APP_ID was just changed, restart the frontend first"
    echo "HINT: if it still does not appear, rebuild or redeploy the frontend bundle with NEXT_PUBLIC_FEISHU_APP_ID baked in"
  fi
else
  echo "WARN: failed to fetch login page"
fi

payload=$(printf '{"code":"preflight","redirect_uri":"%s"}' "$redirect_uri")
feishu_status="$(curl -s -o /tmp/multica-feishu-preflight.out -w '%{http_code}' \
  -H 'Content-Type: application/json' \
  -d "$payload" \
  "$backend_url/auth/feishu" || true)"

case "$feishu_status" in
  200)
    echo "OK: /auth/feishu accepted the preflight request"
    ;;
  400|401|403|409|502)
    echo "OK: /auth/feishu is reachable and configuration appears loaded (status $feishu_status)"
    ;;
  503)
    echo "WARN: /auth/feishu still reports service unavailable"
    echo "HINT: check FEISHU_APP_ID / FEISHU_APP_SECRET and restart the backend"
    ;;
  *)
    echo "WARN: /auth/feishu returned unexpected status ${feishu_status:-unknown}"
    ;;
esac

if [ "$missing" -ne 0 ]; then
  exit 1
fi

#!/usr/bin/env bash
# Source machine-local AI company env (optional).
set -euo pipefail

MULTICA_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"

# Cron/LaunchAgent shells often ship a minimal PATH; gh/cursor-agent live under ~/.local/bin.
_ai_company_bootstrap_path() {
  local dir
  for dir in \
    "${HOME}/.local/bin" \
    "${HOME}/.homebrew/bin" \
    "${HOME}/.homebrew/sbin" \
    "/opt/homebrew/bin" \
    "/opt/homebrew/sbin" \
    "/usr/local/bin" \
    "${HOME}/.bun/bin" \
    "${HOME}/.codebuddy/bin"; do
    if [ -d "$dir" ] && [[ ":${PATH}:" != *":${dir}:"* ]]; then
      PATH="${dir}:${PATH}"
    fi
  done
  export PATH
}
_ai_company_bootstrap_path

LOCAL_ENV="$MULTICA_ROOT/.ai-company/config/local.env"
PROXY_ENV="$MULTICA_ROOT/.ai-company/config/proxy.env"

_ai_company_proxy_dead() {
  local proxy_url="${1:-}"
  local proxy_host_port proxy_host proxy_port
  [ -z "$proxy_url" ] && return 0
  proxy_host_port="${proxy_url#*://}"
  proxy_host="${proxy_host_port%%:*}"
  proxy_port="${proxy_host_port##*:}"
  if ! curl -fsS --connect-timeout 1 "http://${proxy_host}:${proxy_port}/" >/dev/null 2>&1; then
    return 0
  fi
  return 1
}

_ai_company_sanitize_proxy() {
  local key val
  for key in HTTPS_PROXY HTTP_PROXY ALL_PROXY https_proxy http_proxy all_proxy; do
    val="${!key:-}"
    [ -z "$val" ] && continue
    if _ai_company_proxy_dead "$val"; then
      unset "$key"
    fi
  done
}

if [ -f "$PROXY_ENV" ]; then
  # shellcheck disable=SC1090
  source "$PROXY_ENV"
fi
_ai_company_sanitize_proxy

if [ -f "$LOCAL_ENV" ]; then
  # shellcheck disable=SC1090
  source "$LOCAL_ENV"
fi

WEBHOOK_FILE="$MULTICA_ROOT/.ai-company/config/feishu-webhook.url"
if [ -z "${FEISHU_WEBHOOK_URL:-}" ] && [ -f "$WEBHOOK_FILE" ]; then
  FEISHU_WEBHOOK_URL="$(sed -n '1p' "$WEBHOOK_FILE" | tr -d '[:space:]')"
  export FEISHU_WEBHOOK_URL
fi

FEISHU_BOT_NOTIFY_ENV="$MULTICA_ROOT/.ai-company/config/feishu-bot-notify.env"
if [ -f "$FEISHU_BOT_NOTIFY_ENV" ]; then
  # shellcheck disable=SC1090
  source "$FEISHU_BOT_NOTIFY_ENV"
fi

FEISHU_APPROVAL_ENV="$MULTICA_ROOT/.ai-company/config/feishu-approval.env"
if [ -f "$FEISHU_APPROVAL_ENV" ]; then
  # shellcheck disable=SC1090
  source "$FEISHU_APPROVAL_ENV"
fi

SECOND_BRAIN_FEISHU="${SECOND_BRAIN_FEISHU_JSON:-$HOME/Documents/SecondBrain/10-SYSTEM/control-plane-tunnel/feishu.local.json}"
if [ -z "${FEISHU_WEBHOOK_URL:-}" ] && [ -f "$SECOND_BRAIN_FEISHU" ]; then
  FEISHU_WEBHOOK_URL="$(
    python3 - "$SECOND_BRAIN_FEISHU" <<'PY'
import json
import sys
from pathlib import Path

path = Path(sys.argv[1])
try:
    url = str(json.loads(path.read_text(encoding="utf-8")).get("webhookUrl", "")).strip()
except (OSError, json.JSONDecodeError, AttributeError):
    url = ""
if url and "YOUR_TOKEN" not in url:
    print(url)
PY
  )"
  [ -n "$FEISHU_WEBHOOK_URL" ] && export FEISHU_WEBHOOK_URL
fi

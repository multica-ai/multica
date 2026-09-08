#!/usr/bin/env bash
# Push a plain-text CEO brief to Slack or Feishu (webhook or open-platform bot DM).
set -euo pipefail

_feishu_curl() {
  # Domestic API: bypass HTTP proxy; curl handles TLS better than urllib here.
  curl -sS --fail --noproxy 'open.feishu.cn,feishu.cn,larksuite.com,larkoffice.com' "$@"
}

send_slack() {
  local text="$1"
  local webhook="${SLACK_WEBHOOK_URL:-}"
  [ -n "$webhook" ] || return 0
  python3 - "$webhook" "$text" <<'PY'
import json
import sys
import urllib.request

webhook, text = sys.argv[1], sys.argv[2]
body = json.dumps({"text": text}).encode("utf-8")
req = urllib.request.Request(
    webhook,
    data=body,
    headers={"Content-Type": "application/json"},
    method="POST",
)
with urllib.request.urlopen(req, timeout=30) as resp:
    resp.read()
PY
}

send_feishu_bot_dm() {
  local text="$1"
  local app_id="${FEISHU_BOT_APP_ID:-}"
  local app_secret="${FEISHU_BOT_APP_SECRET:-}"
  local open_id="${FEISHU_BOT_NOTIFY_OPEN_ID:-}"
  [ -n "$app_id" ] && [ -n "$app_secret" ] && [ -n "$open_id" ] || return 0

  local token
  token="$(
    _feishu_curl -X POST "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal" \
      -H "Content-Type: application/json" \
      -d "{\"app_id\":\"$app_id\",\"app_secret\":\"$app_secret\"}" \
    | python3 -c "import json,sys; print(json.load(sys.stdin)['tenant_access_token'])"
  )"

  local payload
  payload="$(python3 - "$open_id" "$text" <<'PY'
import json
import sys

open_id, text = sys.argv[1], sys.argv[2]
print(
    json.dumps(
        {
            "receive_id": open_id,
            "msg_type": "text",
            "content": json.dumps({"text": text}, ensure_ascii=False),
        },
        ensure_ascii=False,
    )
)
PY
)"

  local resp
  resp="$(
    _feishu_curl -X POST \
      "https://open.feishu.cn/open-apis/im/v1/messages?receive_id_type=open_id" \
      -H "Content-Type: application/json" \
      -H "Authorization: Bearer $token" \
      -d "$payload"
  )"
  python3 - "$resp" <<'PY'
import json
import sys

out = json.loads(sys.argv[1])
if out.get("code") != 0:
    raise SystemExit(out.get("msg") or "feishu bot dm failed")
PY
}

send_feishu() {
  local text="$1"
  local webhook="${FEISHU_WEBHOOK_URL:-}"
  [ -n "$webhook" ] || return 0
  local payload
  payload="$(python3 - "$text" <<'PY'
import json
import sys

print(json.dumps({"msg_type": "text", "content": {"text": sys.argv[1]}}, ensure_ascii=False))
PY
)"
  _feishu_curl -X POST "$webhook" \
    -H "Content-Type: application/json" \
    -d "$payload" >/dev/null
}

notify_ceo_brief() {
  local text="${1:?}"
  local errors=0
  if [ -n "${SLACK_WEBHOOK_URL:-}" ]; then
    send_slack "$text" || errors=$((errors + 1))
  fi
  if [ -n "${FEISHU_WEBHOOK_URL:-}" ]; then
    send_feishu "$text" || errors=$((errors + 1))
  fi
  if [ -n "${FEISHU_BOT_APP_ID:-}" ] && [ -n "${FEISHU_BOT_NOTIFY_OPEN_ID:-}" ]; then
    send_feishu_bot_dm "$text" || errors=$((errors + 1))
  fi
  return "$errors"
}

has_ceo_notify_channel() {
  [ -n "${SLACK_WEBHOOK_URL:-}" ] || [ -n "${FEISHU_WEBHOOK_URL:-}" ] || \
    { [ -n "${FEISHU_BOT_APP_ID:-}" ] && [ -n "${FEISHU_BOT_NOTIFY_OPEN_ID:-}" ]; }
}

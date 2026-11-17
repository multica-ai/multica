#!/usr/bin/env bash
set -euo pipefail

BASE_DIR="${MULTICA_USER_HOMES_DIR:-$HOME/multica-user-homes}"

err() {
  echo "$*" >&2
}

emit_json() {
  local payload="$1"
  echo "$payload" | jq -e . >/dev/null
  printf '%s\n' "$payload"
}

if [ "${1:-}" = "" ]; then
  err "用法：$0 <multica_user_id>"
  exit 1
fi

USER_ID="$1"
if ! printf '%s' "$USER_ID" | grep -Eq '^[A-Za-z0-9._-]+$'; then
  err "multica_user_id 格式非法：仅允许字母数字._-"
  exit 1
fi

USER_HOME="${BASE_DIR}/mc-user-${USER_ID}"
TARGET_APP_DIR="${USER_HOME}/Library/Application Support/lark-cli"
TARGET_CONFIG="${USER_HOME}/.lark-cli/config.json"

if [ ! -d "$USER_HOME" ] || [ ! -f "$TARGET_CONFIG" ] || [ ! -f "$TARGET_APP_DIR/master.key.file" ]; then
  err "USER_HOME 未 provision，请先运行 02_user_provision.sh"
  exit 1
fi

shopt -s nullglob
APPSECRET_FILES=("${TARGET_APP_DIR}"/appsecret_*.enc)
shopt -u nullglob
if [ "${#APPSECRET_FILES[@]}" -eq 0 ]; then
  err "USER_HOME 缺少 appsecret_*.enc，请重新 provision"
  exit 1
fi

login_json="$(HOME="$USER_HOME" lark-cli auth login --no-wait --json --domain im,calendar,contact)"
echo "$login_json" | jq -e . >/dev/null

device_code="$(echo "$login_json" | jq -r '.device_code // .deviceCode // empty')"
verification_url="$(echo "$login_json" | jq -r '.verification_url // .verificationUrl // .verification_uri // empty')"
user_code="$(echo "$login_json" | jq -r '.user_code // .userCode // empty')"
expires_in="$(echo "$login_json" | jq -r '.expires_in // .expiresIn // 0')"

if [ -z "$device_code" ] || [ -z "$verification_url" ]; then
  err "auth login 返回缺少 device_code 或 verification_url"
  exit 1
fi

qr_ascii_raw="$(HOME="$USER_HOME" lark-cli auth qrcode "$verification_url" --ascii)"
qr_ascii_b64="$(printf '%s' "$qr_ascii_raw" | base64 | tr -d '\n')"

payload="$(jq -n \
  --arg user_id "$USER_ID" \
  --arg device_code "$device_code" \
  --arg verification_url "$verification_url" \
  --arg user_code "$user_code" \
  --arg qr_ascii "$qr_ascii_b64" \
  --argjson expires_in "$expires_in" \
  '{ok:true,user_id:$user_id,device_code:$device_code,verification_url:$verification_url,user_code:$user_code,expires_in:$expires_in,qr_ascii:$qr_ascii}')"

emit_json "$payload"

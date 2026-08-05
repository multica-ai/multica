#!/usr/bin/env bash
set -euo pipefail

USER_HOMES_DIR="${MULTICA_USER_HOMES_DIR:-$HOME/multica-user-homes}"
EXPECTED_OWNER="$(id -un)"

err() {
  echo "$*" >&2
}

emit_json() {
  local payload="$1"
  echo "$payload" | jq -e . >/dev/null
  printf '%s\n' "$payload"
}

default_home() {
  local user home_line
  user="$(id -un)"
  if home_line="$(dscl . -read "/Users/${user}" NFSHomeDirectory 2>/dev/null)"; then
    printf '%s\n' "${home_line##* }"
    return
  fi
  printf '%s\n' "$HOME"
}

mkdir -p "$USER_HOMES_DIR"
chmod 700 "$USER_HOMES_DIR"

if id "$EXPECTED_OWNER" >/dev/null 2>&1; then
  chown "$EXPECTED_OWNER" "$USER_HOMES_DIR"
fi

DEFAULT_HOME="$(default_home)"
APP_SUPPORT_DIR="${DEFAULT_HOME}/Library/Application Support/lark-cli"
MASTER_KEY_FILE="${APP_SUPPORT_DIR}/master.key.file"
CONFIG_FILE="${DEFAULT_HOME}/.lark-cli/config.json"

if [ ! -f "$MASTER_KEY_FILE" ]; then
  err "请先在 Terminal.app 跑 lark-cli config keychain-downgrade"
  exit 1
fi

if [ ! -f "$CONFIG_FILE" ]; then
  err "未找到 ${CONFIG_FILE}，请先完成 lark-cli 初始化与登录"
  exit 1
fi

APP_ID="$(jq -r '(.apps[0].appId // .appId // .app_id) // empty' "$CONFIG_FILE")"
if [ -z "$APP_ID" ]; then
  err "config.json 中未找到 app_id"
  exit 1
fi

APP_SECRET_ENC_FILE="${APP_SUPPORT_DIR}/appsecret_${APP_ID}.enc"
if [ ! -f "$APP_SECRET_ENC_FILE" ]; then
  err "未找到 app secret 文件：${APP_SECRET_ENC_FILE}"
  exit 1
fi

payload="$(jq -n \
  --arg app_id "$APP_ID" \
  --arg user_homes_dir "$USER_HOMES_DIR" \
  --arg master_key_file "$MASTER_KEY_FILE" \
  --arg appsecret_enc_file "$APP_SECRET_ENC_FILE" \
  --arg default_home "$DEFAULT_HOME" \
  '{ok:true,app_id:$app_id,user_homes_dir:$user_homes_dir,master_key_file:$master_key_file,appsecret_enc_file:$appsecret_enc_file,default_home:$default_home}')"

emit_json "$payload"

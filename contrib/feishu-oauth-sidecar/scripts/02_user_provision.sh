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

default_home() {
  local user home_line
  user="$(id -un)"
  if home_line="$(dscl . -read "/Users/${user}" NFSHomeDirectory 2>/dev/null)"; then
    printf '%s\n' "${home_line##* }"
    return
  fi
  printf '%s\n' "$HOME"
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

DEFAULT_HOME="$(default_home)"
SRC_APP_DIR="${DEFAULT_HOME}/Library/Application Support/lark-cli"
SRC_CONFIG="${DEFAULT_HOME}/.lark-cli/config.json"
SRC_MASTER_KEY="${SRC_APP_DIR}/master.key.file"

if [ ! -d "$BASE_DIR" ]; then
  err "未找到 ${BASE_DIR}，请先运行 01_mini_init.sh"
  exit 1
fi

if [ ! -f "$SRC_CONFIG" ] || [ ! -f "$SRC_MASTER_KEY" ]; then
  err "默认 HOME 下缺少 lark-cli 配置或 master.key.file，请先运行 01_mini_init.sh"
  exit 1
fi

if [ -d "$USER_HOME" ]; then
  token_count="$(find "$USER_HOME/Library/Application Support/lark-cli" -type f -name '*.enc' 2>/dev/null | grep -v '/appsecret_' || true | wc -l | tr -d ' ')"
  if [ "${token_count:-0}" -gt 0 ]; then
    payload="$(jq -n --arg user_id "$USER_ID" --arg home "$USER_HOME" '{ok:true,user_id:$user_id,home:$home,ready_for_oauth:true,already_provisioned:true}')"
    emit_json "$payload"
    exit 0
  fi
fi

mkdir -p "$USER_HOME/.lark-cli"
mkdir -p "$USER_HOME/Library/Application Support/lark-cli"
chmod 700 "$USER_HOME" "$USER_HOME/.lark-cli" "$USER_HOME/Library" "$USER_HOME/Library/Application Support" "$USER_HOME/Library/Application Support/lark-cli" 2>/dev/null || true

cp "$SRC_MASTER_KEY" "$USER_HOME/Library/Application Support/lark-cli/master.key.file"

shopt -s nullglob
APPSECRET_FILES=("${SRC_APP_DIR}"/appsecret_*.enc)
shopt -u nullglob
if [ "${#APPSECRET_FILES[@]}" -eq 0 ]; then
  err "默认 HOME 下未找到 appsecret_*.enc"
  exit 1
fi

for f in "${APPSECRET_FILES[@]}"; do
  cp "$f" "$USER_HOME/Library/Application Support/lark-cli/"
done

cp "$SRC_CONFIG" "$USER_HOME/.lark-cli/config.json"

payload="$(jq -n --arg user_id "$USER_ID" --arg home "$USER_HOME" '{ok:true,user_id:$user_id,home:$home,ready_for_oauth:true}')"
emit_json "$payload"

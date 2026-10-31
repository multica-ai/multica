#!/usr/bin/env bash
set -euo pipefail

BASE_DIR="${MULTICA_USER_HOMES_DIR:-$HOME/multica-user-homes}"
MAPPING_FILE="${MULTICA_USER_MAPPING_FILE:-$HOME/multica-user-homes/user_mapping.json}"
LARK_BIN="${LARK_BIN:-$HOME/.nvm/versions/node/v22.12.0/bin/lark-cli}"
TIMEOUT_SECONDS=180

err() { echo "$*" >&2; }
emit_json() {
  local payload="$1"
  echo "$payload" | jq -e . >/dev/null
  printf '%s\n' "$payload"
}

# === 参数校验 ===
if [ "${1:-}" = "" ] || [ "${2:-}" = "" ]; then
  err "用法：$0 <multica_user_id> <device_code>"
  exit 1
fi
USER_ID="$1"
DEVICE_CODE="$2"
if ! printf '%s' "$USER_ID" | grep -Eq '^[A-Za-z0-9._-]+$'; then
  err "multica_user_id 格式非法：仅允许字母数字._-"
  exit 1
fi

USER_HOME="${BASE_DIR}/mc-user-${USER_ID}"
if [ ! -d "$USER_HOME" ] || [ ! -f "$USER_HOME/.lark-cli/config.json" ]; then
  err "USER_HOME 未 provision，请先运行 02_user_provision.sh"
  exit 1
fi

# === 临时文件 + trap (cover tmp_map 残留 — Gemini 建议 #2) ===
tmp_out="$(mktemp)"
tmp_err="$(mktemp)"
tmp_map=""
cleanup() {
  rm -f "$tmp_out" "$tmp_err" "${tmp_map:-}"
}
trap cleanup EXIT

# === polling (subshell + 等 token 落地, 不依赖 lark-cli 子进程 exit code) ===
# 历史 bug: lark-cli auth login --device-code 拿到 token 后还会做 ~3min 额外清理操作才 exit
# 子进程 exit 1 但 token 早已落地。直接 watch token enc 文件出现就视为成功 (类似 self-heal 思路)
TOKEN_ENC_DIR="$USER_HOME/Library/Application Support/lark-cli"

# 记录 polling 前的 enc 文件 (排除 appsecret_) — 之后多出的就是 user token
enc_before="$(find "$TOKEN_ENC_DIR" -type f -name '*.enc' 2>/dev/null | grep -v '/appsecret_' || true)"
enc_before_count="$(printf '%s\n' "$enc_before" | grep -c . || true)"

(
  HOME="$USER_HOME" "$LARK_BIN" auth login --device-code "$DEVICE_CODE" --json >"$tmp_out" 2>"$tmp_err"
) &
pid=$!

start_ts="$(date +%s)"
token_landed=0
while kill -0 "$pid" 2>/dev/null; do
  now_ts="$(date +%s)"
  if [ $((now_ts - start_ts)) -ge "$TIMEOUT_SECONDS" ]; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    err "auth login polling 超时（${TIMEOUT_SECONDS}s）"
    exit 1
  fi
  # 每秒检测 token enc 文件 — 落地立即跳出循环 (kill lark-cli 子进程节省时间)
  enc_now_count="$(find "$TOKEN_ENC_DIR" -type f -name '*.enc' 2>/dev/null | grep -v '/appsecret_' | wc -l | tr -d ' ' || echo 0)"
  if [ "$enc_now_count" -gt "$enc_before_count" ]; then
    token_landed=1
    # 优雅 kill: 给 lark-cli 1s flush 时间再 kill
    sleep 1
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    break
  fi
  sleep 1
done

# 如果 lark-cli 自然退出且 token 没落地 → 真失败 (用户取消授权 / 网络中断)
if [ "$token_landed" -ne 1 ]; then
  enc_now_count="$(find "$TOKEN_ENC_DIR" -type f -name '*.enc' 2>/dev/null | grep -v '/appsecret_' | wc -l | tr -d ' ' || echo 0)"
  if [ "$enc_now_count" -le "$enc_before_count" ]; then
    err "lark-cli 退出但 token 未落地 (stderr: $(cat "$tmp_err" | head -c 300))"
    exit 1
  fi
  token_landed=1
fi

# token 已落地。读 tmp_out 看 lark-cli 是否输出了 scope (可能因 kill 早退没输出,降级用空)
poll_json="$(cat "$tmp_out" 2>/dev/null || echo '{}')"
if ! echo "$poll_json" | jq -e . >/dev/null 2>&1; then
  poll_json='{}'
fi
scopes_json="$(echo "$poll_json" | jq -c '.scope // .scopes // []' 2>/dev/null || echo '[]')"

# === 拿 user 信息 (root cause 修复点 — 不用 --json flag,默认就是 JSON) ===
# 之前 bug: lark-cli auth status 没有 --json flag, 用 --json 触发 exit 1 + 输出 Usage 到 stdout
# `|| fallback` 让 $(...) 把 Usage + JSON 拼接,jq 解析失败
status_raw="$(HOME="$USER_HOME" "$LARK_BIN" auth status 2>/dev/null || true)"

if [ -z "$status_raw" ] || ! echo "$status_raw" | jq -e . >/dev/null 2>&1; then
  err "lark-cli auth status 没返回合法 JSON,无法拿 user_open_id (head=$(printf %s "$status_raw" | head -c 100))"
  exit 1
fi

# 兼容 v1.0.41 nested + 顶层 + snake_case
lark_user_open_id="$(echo "$status_raw" | jq -r '.identities.user.openId // .userOpenId // .user_open_id // .open_id // .user.open_id // empty')"
lark_user_name="$(echo "$status_raw" | jq -r '.identities.user.userName // .userName // .lark_user_name // .user_name // .name // .user.name // empty')"

if [ -z "$lark_user_open_id" ] || [ -z "$lark_user_name" ]; then
  err "无法从 auth status 提取 user_open_id 或 user_name (head=$(printf %s "$status_raw" | head -c 200))"
  exit 1
fi

# === Mapping 写入 (加 flock 防并发覆盖 — Gemini 建议 #1) ===
mapping_dir="$(dirname "$MAPPING_FILE")"
mkdir -p "$mapping_dir"

# macOS 默认无 flock,用 mkdir 原子操作做 mutex
# 最多等 30s 拿锁,超时报错(避免死锁)
lock_dir="$MAPPING_FILE.lock.d"
lock_acquired=0
for _ in $(seq 1 30); do
  if mkdir "$lock_dir" 2>/dev/null; then
    lock_acquired=1
    break
  fi
  sleep 1
done
if [ "$lock_acquired" -ne 1 ]; then
  err "mapping 文件锁拿不到 (30s timeout, lock_dir=$lock_dir 可能残留)"
  exit 1
fi
# 增强 trap: cleanup 时一并释放锁
cleanup_with_lock() {
  rmdir "$lock_dir" 2>/dev/null || true
  cleanup
}
trap cleanup_with_lock EXIT

if [ ! -f "$MAPPING_FILE" ]; then
  printf '[]\n' > "$MAPPING_FILE"
fi

if ! jq -e . "$MAPPING_FILE" >/dev/null 2>&1; then
  err "mapping 文件不是合法 JSON：$MAPPING_FILE"
  exit 1
fi

now_iso="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
tmp_map="$(mktemp "${mapping_dir}/user_mapping.json.tmp.XXXXXX")"

jq --arg uid "$USER_ID" \
   --arg openid "$lark_user_open_id" \
   --arg uname "$lark_user_name" \
   --arg home "$USER_HOME" \
   --arg now "$now_iso" \
   '
   (map(select(.multica_user_id != $uid)))
   + [
       {
         multica_user_id: $uid,
         lark_user_open_id: $openid,
         lark_user_name: $uname,
         home: $home,
         provisioned_at: $now
       }
     ]
   ' "$MAPPING_FILE" > "$tmp_map"

mv "$tmp_map" "$MAPPING_FILE"
tmp_map=""  # 防 cleanup 再删
# 锁会在 EXIT trap 里释放

payload="$(jq -n \
  --arg multica_user_id "$USER_ID" \
  --arg lark_user_open_id "$lark_user_open_id" \
  --arg lark_user_name "$lark_user_name" \
  --argjson scopes "$scopes_json" \
  '{ok:true,multica_user_id:$multica_user_id,lark_user_open_id:$lark_user_open_id,lark_user_name:$lark_user_name,scopes:$scopes}')"

emit_json "$payload"

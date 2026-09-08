#!/usr/bin/env bash
# Expose CEO Feishu approval callback (:9478) via Cloudflare Tunnel.
#
# Quick (no account): random trycloudflare.com URL — good for first Feishu setup.
# Named (stable URL): Cloudflare account + domain — see setup-named.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=lib/source-local-env.sh
source "$SCRIPT_DIR/lib/source-local-env.sh"

LABEL="com.multica.ceo-feishu-cloudflare"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
LOG_FILE="${CEO_FEISHU_CF_TUNNEL_LOG:-$HOME/.multica/ceo-feishu-cloudflare.log}"
URL_FILE="${CEO_FEISHU_CF_TUNNEL_URL_FILE:-$HOME/.multica/ceo-feishu-cloudflare-url.txt}"
CONFIG_DIR="$MULTICA_ROOT/.ai-company/config"
NAMED_CONFIG="$CONFIG_DIR/cloudflared-ceo-feishu.yml"
PORT="${CEO_FEISHU_APPROVAL_PORT:-9478}"
CLOUDFLARED="${CLOUDFLARED_BIN:-$(command -v cloudflared || true)}"
RUN_TOKEN_FILE="${CEO_CLOUDFLARE_RUN_TOKEN_FILE:-$HOME/.multica/cloudflared-run-token}"
PROXY_ENV_BLOCK=""

proxy_env_for_plist() {
  PROXY_ENV_BLOCK=""
  local proxy_file="$MULTICA_ROOT/.ai-company/config/proxy.env"
  if [ -f "$proxy_file" ]; then
    # shellcheck disable=SC1090
    source "$proxy_file"
    if [ -n "${https_proxy:-}" ]; then
      local host_port="${https_proxy#*://}"
      local host="${host_port%%:*}"
      local port="${host_port##*:}"
      if curl -fsS --connect-timeout 2 "http://${host}:${port}/" >/dev/null 2>&1 \
        || curl -sS --connect-timeout 2 -o /dev/null -w '' "http://${host}:${port}/" 2>/dev/null; then
        PROXY_ENV_BLOCK=$(
          cat <<PEOF
		<key>https_proxy</key>
		<string>${https_proxy}</string>
		<key>http_proxy</key>
		<string>${http_proxy:-$https_proxy}</string>
		<key>all_proxy</key>
		<string>${all_proxy:-}</string>
		<key>no_proxy</key>
		<string>127.0.0.1,localhost</string>
PEOF
        )
        echo "  使用代理: ${https_proxy}" >&2
      fi
    fi
  fi
}

usage() {
  cat <<EOF
Usage: ceo-feishu-cloudflare-tunnel.sh <command>

Commands:
  quick              Start quick tunnel; print public URL (trycloudflare.com)
  quick-install      LaunchAgent: quick tunnel (URL changes on restart)
  refresh-quick-url  Update URL file from tunnel log (after restart)
  token-install      LaunchAgent: tunnel run token (file or CLOUDFLARE_TUNNEL_TOKEN)
  fetch-run-token    Account API Token → scoped run token file (REST API)
  verify-api-token   Verify API token + list tunnels
  discover-tunnels   Find tunnels via DNS (when API list is empty)
  probe-api-permissions  Test tunnel list/create/read permissions
  use-configured-run-token  Copy CLOUDFLARE_TUNNEL_TOKEN → run token file
  login              cloudflared tunnel login (for stable hostname)
  setup-named        Print steps to create a named tunnel + config template
  named-install      LaunchAgent: named tunnel (\$NAMED_CONFIG)
  status             Tunnel + approval callback health
  logs               tail tunnel log
  uninstall          Remove LaunchAgent

Feishu open platform → 事件订阅:
  Request URL: https://<public-host>/feishu/event
  Events: card.action.trigger, im.message.receive_v1

Prereq: bash scripts/ai-company/ceo-feishu-approval-service.sh install
EOF
}

need_cloudflared() {
  if [ -z "$CLOUDFLARED" ] || [ ! -x "$CLOUDFLARED" ]; then
    echo "error: cloudflared not found — brew install cloudflared" >&2
    exit 1
  fi
}

resolve_cloudflare_api_token() {
  local t="${CLOUDFLARE_ACCOUNT_API_TOKEN:-}"
  if [ -z "$t" ] || [[ "$t" == YOUR_* ]]; then
    t="${CLOUDFLARE_API_TOKEN:-}"
  fi
  if [ -z "$t" ] || [[ "$t" == YOUR_* ]]; then
    echo "error: set CLOUDFLARE_ACCOUNT_API_TOKEN (or CLOUDFLARE_API_TOKEN) in local.env" >&2
    echo "  Account API Token — 在 dash.cloudflare.com/profile/api-tokens 创建" >&2
    echo "  权限建议: Account → Cloudflare One Connectors → Cloudflare Tunnel → Read（换 token）或 Edit（可创建 tunnel）" >&2
    exit 1
  fi
  printf '%s' "$t"
}

cf_api() {
  local api="$1"
  local url="$2"
  local method="${3:-GET}"
  local body="${4:-}"
  local -a curl_args=(-sS -H "Authorization: Bearer $api" -H "Content-Type: application/json")
  if [ "$method" = POST ]; then
    curl_args+=(-X POST -d "$body")
  fi
  local out
  if ! out="$(curl "${curl_args[@]}" "$url")"; then
  local code=$?
    if [ -n "$out" ]; then
      echo "$out"
      return 0
    fi
    return "$code"
  fi
  echo "$out"
}

cf_json_field() {
  python3 - "$1" "$2" <<'PY'
import json, sys
data = json.loads(sys.argv[1])
path = sys.argv[2].split(".")
cur = data
for key in path:
    if isinstance(cur, dict):
        cur = cur.get(key)
    else:
        cur = None
        break
if cur is None:
    sys.exit(1)
print(cur)
PY
}

cf_api_errors() {
  python3 - "$1" <<'PY'
import json, sys
data = json.loads(sys.argv[1])
errs = data.get("errors") or []
if errs:
    print("; ".join(str(e.get("message", e)) for e in errs))
PY
}

cf_api_success() {
  python3 - "$1" <<'PY'
import json, sys
if not json.loads(sys.argv[1]).get("success"):
    sys.exit(1)
PY
}

resolve_cloudflare_account_id() {
  local api="$1"
  if [ -n "${CLOUDFLARE_ACCOUNT_ID:-}" ]; then
    printf '%s' "$CLOUDFLARE_ACCOUNT_ID"
    return
  fi
  local resp
  resp="$(cf_api "$api" "https://api.cloudflare.com/client/v4/accounts")"
  if ! cf_json_field "$resp" "success" >/dev/null 2>&1; then
    echo "error: list accounts failed: $(cf_api_errors "$resp")" >&2
    exit 1
  fi
  python3 - "$resp" <<'PY'
import json, sys
d = json.loads(sys.argv[1])
accounts = d.get("result") or []
if not accounts:
    raise SystemExit("error: no Cloudflare accounts visible to this API token")
print(accounts[0]["id"])
PY
}

find_tunnel_id_by_name() {
  local api="$1"
  local account_id="$2"
  local name="$3"
  local resp
  resp="$(cf_api "$api" "https://api.cloudflare.com/client/v4/accounts/${account_id}/cfd_tunnel?is_deleted=false")"
  python3 - "$resp" "$name" <<'PY'
import json, sys
d = json.loads(sys.argv[1])
name = sys.argv[2]
for t in d.get("result") or []:
    if t.get("name") == name:
        print(t["id"])
        break
PY
}

need_approval_up() {
  if ! curl -fsS --max-time 2 "http://127.0.0.1:$PORT/health" >/dev/null 2>&1; then
    echo "error: approval server not on :$PORT — bash $SCRIPT_DIR/ceo-feishu-approval-service.sh install" >&2
    exit 1
  fi
}

capture_quick_url() {
  need_cloudflared
  need_approval_up
  rm -f "$LOG_FILE"
  local -a cf_args=(tunnel --protocol http2 --url "http://127.0.0.1:$PORT")
  if [ -f "$MULTICA_ROOT/.ai-company/config/proxy.env" ]; then
    # shellcheck disable=SC1090
    source "$MULTICA_ROOT/.ai-company/config/proxy.env"
  fi
  "$CLOUDFLARED" "${cf_args[@]}" >>"$LOG_FILE" 2>&1 &
  local pid=$!
  echo "cloudflared pid: $pid (log: $LOG_FILE)"
  local url=""
  for _ in $(seq 1 45); do
    url="$(grep -oE 'https://[a-z0-9-]+\.trycloudflare\.com' "$LOG_FILE" 2>/dev/null | head -1 || true)"
    [ -n "$url" ] && break
    sleep 1
  done
  if [ -z "$url" ]; then
    echo "error: could not read trycloudflare URL from log within 45s" >&2
    kill "$pid" 2>/dev/null || true
    exit 1
  fi
  echo "$url" >"$URL_FILE"
  echo ""
  echo "公网 URL: $url"
  echo "飞书 Request URL: ${url}/feishu/event"
  echo "已写入: $URL_FILE"
  echo ""
  echo "按 Ctrl+C 停止 quick tunnel（pid $pid）"
  wait "$pid" || true
}

show_quick_url() {
  if [ -f "$URL_FILE" ]; then
    local url
    url="$(tr -d '[:space:]' <"$URL_FILE")"
    echo "公网 URL: $url"
    echo "飞书 Request URL: ${url}/feishu/event"
  else
    echo "尚无 URL — 先跑: bash $0 quick-install 或 bash $0 refresh-quick-url"
  fi
}

cmd_refresh_quick_url() {
  local url=""
  if [ -f "$LOG_FILE" ]; then
    url="$(grep -oE 'https://[a-z0-9-]+\.trycloudflare\.com' "$LOG_FILE" 2>/dev/null | tail -1 || true)"
  fi
  if [ -z "$url" ]; then
    echo "error: no trycloudflare URL in $LOG_FILE — is quick tunnel running?" >&2
    exit 1
  fi
  echo "$url" >"$URL_FILE"
  echo "  ✅ URL 已刷新: $url"
  show_quick_url
}

generate_quick_plist() {
  need_cloudflared
  proxy_env_for_plist
  cat >"$PLIST" <<PEOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>$LABEL</string>
	<key>ProgramArguments</key>
	<array>
		<string>$CLOUDFLARED</string>
		<string>tunnel</string>
		<string>--protocol</string>
		<string>http2</string>
		<string>--url</string>
		<string>http://127.0.0.1:$PORT</string>
	</array>
	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key>
		<string>$HOME/.homebrew/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string>
$PROXY_ENV_BLOCK
	</dict>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	<string>$LOG_FILE</string>
	<key>StandardErrorPath</key>
	<string>$LOG_FILE</string>
	<key>ProcessType</key>
	<string>Background</string>
</dict>
</plist>
PEOF
}

cmd_quick_install() {
  echo "📦 安装 Cloudflare quick tunnel LaunchAgent..."
  generate_quick_plist
  launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
  launchctl bootstrap "gui/$(id -u)" "$PLIST"
  echo "  ✅ LaunchAgent 已安装"
  echo "  📝 日志: tail -f $LOG_FILE"
  echo "  ⚠️  quick tunnel URL 每次重启会变 — 变后重跑: bash $0 quick-url"
  sleep 15
  grep -oE 'https://[a-z0-9-]+\.trycloudflare\.com' "$LOG_FILE" 2>/dev/null | tail -1 | tee "$URL_FILE" || true
  show_quick_url
}

cmd_setup_named() {
  cat <<EOF

=== 稳定域名（Named Tunnel）===

1) 登录 Cloudflare（会打开浏览器）:
   $CLOUDFLARED tunnel login

2) 创建隧道:
   $CLOUDFLARED tunnel create ceo-feishu-approval

3) 在 Cloudflare DNS 添加 CNAME（Dashboard → Zero Trust → Tunnels）:
   feishu-ceo.<你的域名> → <tunnel-id>.cfargotunnel.com

4) 写入配置 $NAMED_CONFIG（替换 TUNNEL_ID 与 hostname）:

tunnel: <TUNNEL_ID>
credentials-file: $HOME/.cloudflared/<TUNNEL_ID>.json
ingress:
  - hostname: feishu-ceo.example.com
    service: http://127.0.0.1:$PORT
  - service: http_status:404

5) 安装常开:
   bash $0 named-install

6) 飞书 Request URL:
   https://feishu-ceo.<你的域名>/feishu/event

EOF
  if [ ! -f "$NAMED_CONFIG" ]; then
    mkdir -p "$CONFIG_DIR"
    cat >"$NAMED_CONFIG.example" <<EXEOF
# Copy to cloudflared-ceo-feishu.yml and fill TUNNEL_ID + hostname
tunnel: <TUNNEL_ID>
credentials-file: $HOME/.cloudflared/<TUNNEL_ID>.json
ingress:
  - hostname: feishu-ceo.example.com
    service: http://127.0.0.1:$PORT
  - service: http_status:404
EXEOF
    echo "模板已写: $NAMED_CONFIG.example"
  fi
}

generate_named_plist() {
  need_cloudflared
  [ -f "$NAMED_CONFIG" ] || {
    echo "error: missing $NAMED_CONFIG — bash $0 setup-named" >&2
    exit 1
  }
  cat >"$PLIST" <<PEOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>$LABEL</string>
	<key>ProgramArguments</key>
	<array>
		<string>$CLOUDFLARED</string>
		<string>tunnel</string>
		<string>--config</string>
		<string>$NAMED_CONFIG</string>
		<string>run</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	<string>$LOG_FILE</string>
	<key>StandardErrorPath</key>
	<string>$LOG_FILE</string>
	<key>ProcessType</key>
	<string>Background</string>
</dict>
</plist>
PEOF
}

cmd_named_install() {
  echo "📦 安装 Cloudflare named tunnel LaunchAgent..."
  generate_named_plist
  launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
  launchctl bootstrap "gui/$(id -u)" "$PLIST"
  echo "  ✅ named tunnel LaunchAgent 已安装"
  echo "  飞书 URL: https://<你的hostname>/feishu/event"
}

generate_token_plist() {
  need_cloudflared
  local token="${CLOUDFLARE_TUNNEL_TOKEN:-${TUNNEL_TOKEN:-}}"
  local use_token_file=0
  local token_file_arg=""

  if [ -f "$RUN_TOKEN_FILE" ] && [ -s "$RUN_TOKEN_FILE" ]; then
    use_token_file=1
    token_file_arg="$RUN_TOKEN_FILE"
  elif [ -n "$token" ] && [[ "$token" != YOUR_* ]]; then
    use_token_file=0
  else
    echo "error: no tunnel run credential" >&2
    echo "  推荐: 在 local.env 设 CLOUDFLARE_API_TOKEN + CLOUDFLARE_TUNNEL_NAME，然后:" >&2
    echo "    bash $0 fetch-run-token" >&2
    echo "  或设 CLOUDFLARE_TUNNEL_TOKEN（仅 tunnel 的 run token，不是 Account API Token）" >&2
    exit 1
  fi

  proxy_env_for_plist
  if [ "$use_token_file" -eq 1 ]; then
    cat >"$PLIST" <<PEOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>$LABEL</string>
	<key>ProgramArguments</key>
	<array>
		<string>$CLOUDFLARED</string>
		<string>tunnel</string>
		<string>--protocol</string>
		<string>http2</string>
		<string>run</string>
		<string>--token-file</string>
		<string>$token_file_arg</string>
	</array>
	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key>
		<string>$HOME/.homebrew/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string>
$PROXY_ENV_BLOCK
	</dict>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	<string>$LOG_FILE</string>
	<key>StandardErrorPath</key>
	<string>$LOG_FILE</string>
	<key>ProcessType</key>
	<string>Background</string>
</dict>
</plist>
PEOF
  else
    cat >"$PLIST" <<PEOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>$LABEL</string>
	<key>ProgramArguments</key>
	<array>
		<string>$CLOUDFLARED</string>
		<string>tunnel</string>
		<string>--protocol</string>
		<string>http2</string>
		<string>run</string>
		<string>--token</string>
		<string>$token</string>
	</array>
	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key>
		<string>$HOME/.homebrew/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string>
$PROXY_ENV_BLOCK
	</dict>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	<string>$LOG_FILE</string>
	<key>StandardErrorPath</key>
	<string>$LOG_FILE</string>
	<key>ProcessType</key>
	<string>Background</string>
</dict>
</plist>
PEOF
  fi
}

cmd_token_install() {
  echo "📦 安装 Cloudflare tunnel LaunchAgent..."
  generate_token_plist
  launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
  launchctl bootstrap "gui/$(id -u)" "$PLIST"
  echo "  ✅ token tunnel LaunchAgent 已安装"
  echo "  在 Zero Trust 把 public hostname 指到 http://127.0.0.1:$PORT"
  echo "  飞书 Request URL: https://<你的固定域名>/feishu/event"
}

cmd_fetch_run_token() {
  local api name account_id tunnel_id resp run_token
  api="$(resolve_cloudflare_api_token)"
  name="${CLOUDFLARE_TUNNEL_NAME:-ceo-feishu-approval}"
  account_id="$(resolve_cloudflare_account_id "$api")"
  echo ">> Cloudflare API: account ${account_id}, tunnel name ${name}"

  tunnel_id="$(find_tunnel_id_by_name "$api" "$account_id" "$name")"
  if [ -z "$tunnel_id" ] && [ -n "${CLOUDFLARE_TUNNEL_ID:-}" ]; then
    tunnel_id="$CLOUDFLARE_TUNNEL_ID"
    echo "  使用 CLOUDFLARE_TUNNEL_ID=${tunnel_id}" >&2
  fi
  if [ -z "$tunnel_id" ]; then
    echo "error: tunnel '${name}' not found via API" >&2
    echo "  你的 tunnel 很可能已存在，但 API Token 缺少 Cloudflare Tunnel Read 权限（列表会显示 0）" >&2
    echo "  运行: bash $0 discover-tunnels  查看 DNS 里已有的 tunnel" >&2
    echo "  然后二选一:" >&2
    echo "    A) Zero Trust → 现有 tunnel → Configure → 复制 run token → CLOUDFLARE_TUNNEL_TOKEN" >&2
    echo "       → bash $0 use-configured-run-token && bash $0 token-install" >&2
    echo "    B) 给 API Token 加 Account → Cloudflare Tunnel Read，再 bash $0 fetch-run-token" >&2
    exit 1
  fi

  echo ">> GET tunnel token (tunnel_id=${tunnel_id})"
  resp="$(cf_api "$api" "https://api.cloudflare.com/client/v4/accounts/${account_id}/cfd_tunnel/${tunnel_id}/token")"
  if ! cf_api_success "$resp"; then
    echo "error: fetch token failed: $(cf_api_errors "$resp")" >&2
    exit 1
  fi
  run_token="$(cf_json_field "$resp" "result")"

  mkdir -p "$(dirname "$RUN_TOKEN_FILE")"
  printf '%s' "$run_token" >"$RUN_TOKEN_FILE"
  chmod 600 "$RUN_TOKEN_FILE"
  echo "  ✅ scoped run token (${#run_token} chars) → $RUN_TOKEN_FILE"
  echo "  ⚠️  可注释 local.env 里的 CLOUDFLARE_ACCOUNT_API_TOKEN（LaunchAgent 只用 run token 文件）"
  echo "  下一步: bash $0 token-install"
}

cmd_create_tunnel() {
  local api name account_id resp tunnel_id run_token
  api="$(resolve_cloudflare_api_token)"
  name="${CLOUDFLARE_TUNNEL_NAME:-ceo-feishu-approval}"
  account_id="$(resolve_cloudflare_account_id "$api")"
  echo ">> POST create tunnel ${name}"
  resp="$(cf_api "$api" "https://api.cloudflare.com/client/v4/accounts/${account_id}/cfd_tunnel" POST "{\"name\":\"${name}\",\"config_src\":\"cloudflare\"}")"
  if ! cf_api_success "$resp"; then
    echo "error: create tunnel failed: $(cf_api_errors "$resp")" >&2
    echo "  当前 Token 实测无 Tunnel Write（403 Authentication error）" >&2
    echo "  运行: bash $0 probe-api-permissions" >&2
    echo "  在 dash.cloudflare.com/profile/api-tokens 创建 Custom Token:" >&2
    echo "    Account → Cloudflare Tunnel → Edit（或 Cloudflare One Connector: cloudflared → Edit）" >&2
    echo "    Zone → DNS → Edit（配 Public Hostname 时需要）" >&2
    exit 1
  fi
  tunnel_id="$(cf_json_field "$resp" "result.id")"
  run_token="$(python3 - "$resp" <<'PY'
import json, sys
r = json.loads(sys.argv[1]).get("result") or {}
print(r.get("token") or "")
PY
)"
  echo "  ✅ tunnel created: ${name} (${tunnel_id})"
  if [ -n "$run_token" ]; then
    mkdir -p "$(dirname "$RUN_TOKEN_FILE")"
    printf '%s' "$run_token" >"$RUN_TOKEN_FILE"
    chmod 600 "$RUN_TOKEN_FILE"
    echo "  ✅ run token (${#run_token} chars) → $RUN_TOKEN_FILE"
    echo "  下一步: Zero Trust 配 Public Hostname → http://127.0.0.1:${PORT}"
    echo "  然后: bash $0 token-install"
  else
    echo "  下一步: Zero Trust 配 Public Hostname → http://127.0.0.1:${PORT}"
    echo "  然后: bash $0 fetch-run-token && bash $0 token-install"
  fi
}

cmd_probe_api_permissions() {
  local api account_id resp probe_name
  api="$(resolve_cloudflare_api_token)"
  account_id="$(resolve_cloudflare_account_id "$api")"
  probe_name="ceo-feishu-probe-$(date +%s)"
  echo ">> Cloudflare API Token 权限探测（account ${account_id}）"

  resp="$(cf_api "$api" "https://api.cloudflare.com/client/v4/user/tokens/verify")"
  if cf_api_success "$resp"; then
    echo "  ✅ Token 有效"
  else
    echo "  ❌ Token verify 失败: $(cf_api_errors "$resp")"
    exit 1
  fi

  resp="$(cf_api "$api" "https://api.cloudflare.com/client/v4/zones?per_page=1")"
  if cf_api_success "$resp"; then
    echo "  ✅ Zone 列表可读"
  else
    echo "  ❌ Zone 列表失败"
  fi

  resp="$(cf_api "$api" "https://api.cloudflare.com/client/v4/accounts/${account_id}/cfd_tunnel?per_page=1")"
  count="$(python3 - "$resp" <<'PY'
import json,sys
d=json.loads(sys.argv[1])
print(len(d.get("result") or []))
PY
)"
  if cf_api_success "$resp"; then
    if [ "$count" = 0 ]; then
      echo "  ⚠️  Tunnel 列表为空 — 可能无 Tunnel Read（DNS 里已有 tunnel 时见 discover-tunnels）"
    else
      echo "  ✅ Tunnel 列表可读（${count}+ 条）"
    fi
  else
    echo "  ❌ Tunnel 列表 API 失败: $(cf_api_errors "$resp")"
  fi

  resp="$(cf_api "$api" "https://api.cloudflare.com/client/v4/accounts/${account_id}/cfd_tunnel/ada783e7-ad4c-4f7b-a9d8-6ede18b2e1ae/token")"
  if cf_api_success "$resp"; then
    echo "  ✅ Tunnel token 可读（ctrl tunnel）"
  else
    echo "  ❌ Tunnel token 读取: $(cf_api_errors "$resp")"
  fi

  echo ">> 试探创建 tunnel (${probe_name})…"
  resp="$(cf_api "$api" "https://api.cloudflare.com/client/v4/accounts/${account_id}/cfd_tunnel" POST "{\"name\":\"${probe_name}\",\"config_src\":\"cloudflare\"}")"
  if cf_api_success "$resp"; then
    tunnel_id="$(cf_json_field "$resp" "result.id")"
    echo "  ✅ Tunnel 创建成功（probe ${tunnel_id}）— 将删除 probe tunnel"
    cf_api "$api" "https://api.cloudflare.com/client/v4/accounts/${account_id}/cfd_tunnel/${tunnel_id}" POST "{\"name\":\"${probe_name}\",\"config_src\":\"cloudflare\"}" >/dev/null 2>&1 || true
    # DELETE if supported
    curl -sS -X DELETE -H "Authorization: Bearer $api" \
      "https://api.cloudflare.com/client/v4/accounts/${account_id}/cfd_tunnel/${tunnel_id}" >/dev/null 2>&1 || true
    echo "  → 可运行: bash $0 create-tunnel"
  else
    echo "  ❌ Tunnel 创建失败: $(cf_api_errors "$resp")"
    echo "  需要在 API Token 加 Account → Cloudflare Tunnel → Edit"
  fi
}

cmd_verify_api_token() {
  local api account_id resp
  api="$(resolve_cloudflare_api_token)"
  resp="$(cf_api "$api" "https://api.cloudflare.com/client/v4/user/tokens/verify")"
  if cf_api_success "$resp"; then
    echo "  ✅ Account API Token 有效"
  else
    echo "error: token verify failed: $(cf_api_errors "$resp")" >&2
    exit 1
  fi
  account_id="$(resolve_cloudflare_account_id "$api")"
  resp="$(cf_api "$api" "https://api.cloudflare.com/client/v4/accounts/${account_id}/cfd_tunnel?is_deleted=false")"
  python3 - "$resp" "${CLOUDFLARE_TUNNEL_NAME:-ceo-feishu-approval}" <<'PY'
import json, sys
d = json.loads(sys.argv[1])
want = sys.argv[2]
tunnels = d.get("result") or []
print(f"  account tunnels: {len(tunnels)}")
for t in tunnels:
    mark = " ← target" if t.get("name") == want else ""
    print(f"    - {t.get('name')} ({t.get('id')}){mark}")
if not any(t.get("name") == want for t in tunnels):
    print(f"  ⚠️  API 列表里没有 '{want}'")
    if not tunnels:
        print("  ⚠️  列表为空常见于 Token 缺 Cloudflare Tunnel Read — 试 bash $0 discover-tunnels")
PY
}

cmd_discover_tunnels() {
  local api account_id
  api="$(resolve_cloudflare_api_token)"
  account_id="$(resolve_cloudflare_account_id "$api")"
  echo ">> DNS 扫描 cfargotunnel.com（账户 ${account_id}）"
  local zones_resp
  zones_resp="$(cf_api "$api" "https://api.cloudflare.com/client/v4/zones?per_page=50")"
  python3 - "$zones_resp" "$api" <<'PY'
import json, subprocess, sys, re

zones = json.loads(sys.argv[1]).get("result") or []
api = sys.argv[2]
tunnel_hosts: dict[str, list[str]] = {}

for z in zones:
    zid = z.get("id")
    zname = z.get("name")
    if not zid:
        continue
    raw = subprocess.check_output([
        "curl", "-sS", "-H", f"Authorization: Bearer {api}",
        f"https://api.cloudflare.com/client/v4/zones/{zid}/dns_records?per_page=100",
    ])
    records = json.loads(raw).get("result") or []
    for r in records:
        if r.get("type") != "CNAME":
            continue
        content = (r.get("content") or "").strip().lower()
        m = re.match(r"^([0-9a-f-]{36})\.cfargotunnel\.com\.?$", content)
        if not m:
            continue
        tid = m.group(1)
        host = r.get("name", "")
        tunnel_hosts.setdefault(tid, []).append(f"{host} ({zname})")

if not tunnel_hosts:
    print("  未发现 cfargotunnel.com CNAME — 检查 API Token 是否有 Zone DNS Read")
    raise SystemExit(0)

print(f"  发现 {len(tunnel_hosts)} 个 tunnel（来自 DNS，非 API 列表）:")
for tid, hosts in sorted(tunnel_hosts.items(), key=lambda x: x[1][0]):
    print(f"  tunnel_id={tid}")
    for h in hosts:
        print(f"    - {h}")
print()
print("  复用现有 tunnel（推荐）:")
print("    1. Zero Trust → Networks → Tunnels → 选上表 tunnel → Configure")
print("    2. Public Hostname 加一条 → http://127.0.0.1:9478")
print("    3. Install connector 复制 run token → local.env CLOUDFLARE_TUNNEL_TOKEN")
print("    4. bash scripts/ai-company/ceo-feishu-cloudflare-tunnel.sh use-configured-run-token")
print("    5. bash scripts/ai-company/ceo-feishu-cloudflare-tunnel.sh token-install")
PY
}

cmd_use_configured_run_token() {
  local t="${CLOUDFLARE_TUNNEL_TOKEN:-}"
  if [ -z "$t" ] || [[ "$t" == YOUR_* ]]; then
    if [[ "${CLOUDFLARE_API_TOKEN:-}" == cfut_* ]]; then
      t="$CLOUDFLARE_API_TOKEN"
      echo "  使用 CLOUDFLARE_API_TOKEN 中的 tunnel run token（建议改写到 CLOUDFLARE_TUNNEL_TOKEN）" >&2
    else
      echo "error: set CLOUDFLARE_TUNNEL_TOKEN (cfut_...) in local.env" >&2
      exit 1
    fi
  fi
  mkdir -p "$(dirname "$RUN_TOKEN_FILE")"
  printf '%s' "$t" >"$RUN_TOKEN_FILE"
  chmod 600 "$RUN_TOKEN_FILE"
  echo "  ✅ run token (${#t} chars) → $RUN_TOKEN_FILE"
  echo "  下一步: bash $0 token-install"
}

cmd_uninstall() {
  launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
  rm -f "$PLIST"
  echo "  ✅ tunnel LaunchAgent 已卸载"
}

cmd_status() {
  echo "📊 Cloudflare tunnel + 飞书回调"
  if launchctl print "gui/$(id -u)/$LABEL" &>/dev/null; then
    PID="$(launchctl print "gui/$(id -u)/$LABEL" 2>/dev/null | awk '/pid =/ {print $3; exit}')"
    echo "  🟢 tunnel LaunchAgent (pid ${PID:-?})"
  else
    pgrep -fl cloudflared >/dev/null && echo "  🟡 cloudflared 手动进程" || echo "  ⚪ tunnel 未运行"
  fi
  bash "$SCRIPT_DIR/ceo-feishu-approval-service.sh" status
  show_quick_url
  if [ -f "$URL_FILE" ]; then
    local url pub_ok=0
    url="$(tr -d '[:space:]' <"$URL_FILE")"
    if [ -n "$url" ] && curl -fsS --max-time 15 "${url}/health" >/dev/null 2>&1; then
      echo "  🟢 公网 /health 可达"
    elif [ -n "$url" ]; then
      echo "  🔴 公网 /health 不可达 — 检查 proxy.env 后重装 quick-install"
    fi
  fi
}

cmd_logs() {
  tail -f "$LOG_FILE"
}

case "${1:-}" in
  quick) capture_quick_url ;;
  quick-install) cmd_quick_install ;;
  quick-url) show_quick_url ;;
  refresh-quick-url) cmd_refresh_quick_url ;;
  login) need_cloudflared; "$CLOUDFLARED" tunnel login ;;
  setup-named) cmd_setup_named ;;
  named-install) cmd_named_install ;;
  token-install) cmd_token_install ;;
  fetch-run-token) cmd_fetch_run_token ;;
  create-tunnel) cmd_create_tunnel ;;
  verify-api-token) cmd_verify_api_token ;;
  discover-tunnels) cmd_discover_tunnels ;;
  probe-api-permissions) cmd_probe_api_permissions ;;
  use-configured-run-token) cmd_use_configured_run_token ;;
  uninstall) cmd_uninstall ;;
  status) cmd_status ;;
  logs) cmd_logs ;;
  -h|--help|help|"") usage ;;
  *) echo "Unknown command: $1" >&2; usage >&2; exit 1 ;;
esac

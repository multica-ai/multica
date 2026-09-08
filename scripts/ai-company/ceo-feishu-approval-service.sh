#!/usr/bin/env bash
# Feishu CEO approval callback LaunchAgent — keep :9478 up for card + /批 commands.
# Usage: bash ceo-feishu-approval-service.sh [install|uninstall|start|stop|restart|status|logs]
set -euo pipefail

LABEL="com.multica.ceo-feishu-approval"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
LOG_FILE="${CEO_FEISHU_APPROVAL_LOG:-$HOME/.multica/ceo-feishu-approval.log}"
PYTHON_BIN="$(command -v python3)"

# shellcheck source=lib/source-local-env.sh
source "$SCRIPT_DIR/lib/source-local-env.sh"

PORT="${CEO_FEISHU_APPROVAL_PORT:-9478}"
REGISTRY="${REGISTRY:-$MULTICA_ROOT/.ai-company/templates/project-registry.yaml}"

local_env_plist_block() {
  LOCAL_ENV_BLOCK=""
  local key val
  for key in FEISHU_VERIFICATION_TOKEN GITHUB_ORG MULTICA_FRONTEND_URL REGISTRY; do
    val="${!key:-}"
    if [ -n "$val" ] && [[ "$val" != YOUR_* ]]; then
      LOCAL_ENV_BLOCK+=$(
        cat <<PEOF
		<key>${key}</key>
		<string>${val}</string>
PEOF
      )
    fi
  done
}

generate_plist() {
  local_env_plist_block
  cat >"$PLIST" <<PEOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>$LABEL</string>
	<key>ProgramArguments</key>
	<array>
		<string>$PYTHON_BIN</string>
		<string>$SCRIPT_DIR/ceo-feishu-approval-server.py</string>
		<string>--port</string>
		<string>$PORT</string>
	</array>
	<key>WorkingDirectory</key>
	<string>$MULTICA_ROOT</string>
	<key>EnvironmentVariables</key>
	<dict>
		<key>HOME</key>
		<string>$HOME</string>
		<key>PATH</key>
		<string>$HOME/.local/bin:/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
$LOCAL_ENV_BLOCK
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
  echo "  ✅ plist: $PLIST"
}

cmd_install() {
  echo "📦 安装飞书审批回调服务自启动..."
  generate_plist
  launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
  launchctl bootstrap "gui/$(id -u)" "$PLIST"
  echo "  ✅ http://127.0.0.1:$PORT/feishu/event"
  echo "  📝 日志: tail -f $LOG_FILE"
  echo "  ⚠️  飞书开放平台 Request URL 需公网可达 → https://<host>/feishu/event"
}

cmd_uninstall() {
  launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
  rm -f "$PLIST"
  echo "  ✅ 已卸载"
}

cmd_start() {
  if launchctl print "gui/$(id -u)/$LABEL" &>/dev/null; then
    launchctl kickstart -k "gui/$(id -u)/$LABEL"
    echo "  ✅ 已启动"
  else
    echo "  ⚠️  未安装 — bash $0 install"
  fi
}

cmd_stop() {
  launchctl kill SIGTERM "gui/$(id -u)/$LABEL" 2>/dev/null && echo "  ✅ 已停止" || echo "  ⚠️  未运行"
}

cmd_restart() {
  cmd_stop
  sleep 1
  cmd_start
}

cmd_status() {
  echo "📊 飞书审批回调 (:$PORT)"
  if launchctl print "gui/$(id -u)/$LABEL" &>/dev/null; then
    PID="$(launchctl print "gui/$(id -u)/$LABEL" 2>/dev/null | awk '/pid =/ {print $3; exit}')"
    if [ -n "$PID" ] && [ "$PID" != "0" ]; then
      echo "  🟢 LaunchAgent 运行中 (pid $PID)"
    else
      echo "  🔴 LaunchAgent 已注册但未运行"
    fi
  else
    pgrep -fl ceo-feishu-approval-server.py >/dev/null && echo "  🟡 手动进程在跑（未装 LaunchAgent）" || echo "  ⚪ 未运行"
  fi
  curl -fsS --max-time 2 "http://127.0.0.1:$PORT/health" >/dev/null && echo "  ✅ /health OK" || echo "  ❌ :$PORT 无响应"
  if [ -n "${FEISHU_VERIFICATION_TOKEN:-}" ] && [[ "${FEISHU_VERIFICATION_TOKEN}" != YOUR_* ]]; then
    echo "  ✅ FEISHU_VERIFICATION_TOKEN 已注入 LaunchAgent"
  else
    echo "  ⚠️  FEISHU_VERIFICATION_TOKEN 未设 — 事件校验会 401（URL challenge 仍可用）"
  fi
  url_file="${CEO_FEISHU_CF_TUNNEL_URL_FILE:-$HOME/.multica/ceo-feishu-cloudflare-url.txt}"
  if [ -f "$url_file" ]; then
    pub="$(tr -d '[:space:]' <"$url_file")"
    [ -n "$pub" ] && echo "  飞书 Request URL: ${pub}/feishu/event"
  fi
}

cmd_logs() {
  tail -f "$LOG_FILE"
}

case "${1:-status}" in
  install) cmd_install ;;
  uninstall) cmd_uninstall ;;
  start) cmd_start ;;
  stop) cmd_stop ;;
  restart) cmd_restart ;;
  status) cmd_status ;;
  logs) cmd_logs ;;
  *) echo "Usage: $0 [install|uninstall|start|stop|restart|status|logs]" >&2; exit 1 ;;
esac

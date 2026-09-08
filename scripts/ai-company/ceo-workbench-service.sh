#!/usr/bin/env bash
# CEO workbench LaunchAgent — keep :9477 up for Feishu site-factory intake.
# Usage: bash ceo-workbench-service.sh [install|uninstall|start|stop|restart|status|logs]
set -euo pipefail

LABEL="com.multica.ceo-workbench"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
LOG_FILE="${CEO_WORKBENCH_LOG:-$HOME/.multica/ceo-workbench.log}"
PYTHON_BIN="$(command -v python3)"

generate_plist() {
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
		<string>$SCRIPT_DIR/ceo-workbench-server.py</string>
	</array>
	<key>WorkingDirectory</key>
	<string>$MULTICA_ROOT</string>
	<key>EnvironmentVariables</key>
	<dict>
		<key>HOME</key>
		<string>$HOME</string>
		<key>CEO_WORKBENCH_OPEN_BROWSER</key>
		<string>0</string>
		<key>REGISTRY</key>
		<string>$MULTICA_ROOT/.ai-company/templates/project-registry.yaml</string>
		<key>PATH</key>
		<string>$HOME/.local/bin:/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
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
  echo "📦 安装 CEO workbench 自启动..."
  generate_plist
  launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
  launchctl bootstrap "gui/$(id -u)" "$PLIST"
  echo "  ✅ http://127.0.0.1:9477"
  echo "  📝 日志: tail -f $LOG_FILE"
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
  echo "📊 CEO workbench"
  if launchctl print "gui/$(id -u)/$LABEL" &>/dev/null; then
    PID="$(launchctl print "gui/$(id -u)/$LABEL" 2>/dev/null | awk '/pid =/ {print $3; exit}')"
    if [ -n "$PID" ] && [ "$PID" != "0" ]; then
      echo "  🟢 LaunchAgent 运行中 (pid $PID)"
    else
      echo "  🔴 LaunchAgent 已注册但未运行"
    fi
  else
    pgrep -fl ceo-workbench-server.py >/dev/null && echo "  🟡 手动进程在跑（未装 LaunchAgent）" || echo "  ⚪ 未运行"
  fi
  curl -fsS --max-time 2 http://127.0.0.1:9477/api/health >/dev/null && echo "  ✅ /api/health OK" || echo "  ❌ :9477 无响应"
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

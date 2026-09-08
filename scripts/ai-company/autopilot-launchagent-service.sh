#!/usr/bin/env bash
# macOS LaunchAgent for daytime Employee Autopilot (preferred over cron on Mac).
# Runs in the logged-in GUI session so cursor-agent session auth propagates to nohup dispatch.
# Usage: bash autopilot-launchagent-service.sh [install|uninstall|start|stop|restart|status|logs]
set -euo pipefail

LABEL="com.multica.ai-company-autopilot"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
LOG_FILE="${AUTOPILOT_LOG:-$HOME/.multica/autopilot-launchagent.log}"
WRAPPER="$SCRIPT_DIR/autopilot-launchagent-run.sh"
INTERVAL_SEC="${AUTOPILOT_LAUNCHAGENT_INTERVAL_SEC:-1800}" # 30m; script enforces quiet hours

# shellcheck source=lib/source-local-env.sh
source "$SCRIPT_DIR/lib/source-local-env.sh"

local_env_plist_block() {
  LOCAL_ENV_BLOCK=""
  local key val
  for key in CURSOR_API_KEY GITHUB_ORG CEO_AUTO_MERGE AUTOPILOT_MAX_CONCURRENT AUTOPILOT_MAX_TOTAL; do
    val="${!key:-}"
    if [ -n "$val" ]; then
      LOCAL_ENV_BLOCK+=$(
        cat <<PEOF
		<key>${key}</key>
		<string>${val}</string>
PEOF
      )
    fi
  done
}

write_wrapper() {
  cat >"$WRAPPER" <<EOF
#!/usr/bin/env bash
set -euo pipefail
cd "$MULTICA_ROOT"
# shellcheck source=lib/source-local-env.sh
source "$SCRIPT_DIR/lib/source-local-env.sh"
exec bash "$SCRIPT_DIR/autopilot-dispatch.sh"
EOF
  chmod +x "$WRAPPER"
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
		<string>/bin/bash</string>
		<string>$WRAPPER</string>
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
	<key>StartInterval</key>
	<integer>$INTERVAL_SEC</integer>
	<key>RunAtLoad</key>
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

remove_cron_autopilot() {
  local existing
  existing="$(crontab -l 2>/dev/null || true)"
  if echo "$existing" | grep -q 'autopilot-dispatch.sh'; then
    echo "  crontab: removing autopilot lines (LaunchAgent is authoritative)"
    echo "$existing" | grep -v 'multica-ai-company-autopilot' | grep -v 'autopilot-dispatch.sh' | crontab - || true
  fi
}

cmd_install() {
  echo "📦 安装 Employee Autopilot LaunchAgent（GUI 会话，自主派单）..."
  write_wrapper
  generate_plist
  launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
  launchctl bootstrap "gui/$(id -u)" "$PLIST"
  remove_cron_autopilot
  echo "  ✅ 每 ${INTERVAL_SEC}s 触发 autopilot-dispatch（安静时段 23:00–06:00 脚本内跳过）"
  echo "  📝 日志: tail -f $LOG_FILE"
  echo "  📝 详细: tail -f $HOME/.multica/autopilot-logs/autopilot-*.log"
  echo "  ⚠️  勿再手动 force dispatch；观察上述日志即可"
}

cmd_uninstall() {
  launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
  rm -f "$PLIST" "$WRAPPER"
  echo "  ✅ LaunchAgent 已卸载"
}

cmd_start() {
  if launchctl print "gui/$(id -u)/$LABEL" &>/dev/null; then
    launchctl kickstart -k "gui/$(id -u)/$LABEL"
    echo "  ✅ 已触发一轮 autopilot"
  else
    echo "  ⚠️  未安装 — bash $0 install"
  fi
}

cmd_stop() {
  launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
  echo "  ⏸ 已停止（plist 仍在；install 或 start 可恢复）"
}

cmd_restart() {
  cmd_stop
  sleep 1
  cmd_start
}

cmd_status() {
  if launchctl print "gui/$(id -u)/$LABEL" &>/dev/null; then
    PID="$(launchctl print "gui/$(id -u)/$LABEL" 2>/dev/null | awk -F'= ' '/pid =/{print $2; exit}' || true)"
    echo "  🟢 LaunchAgent 已注册 (last pid ${PID:-?})"
  else
    echo "  ⚪ LaunchAgent 未安装"
  fi
  if pgrep -fl 'autopilot-dispatch.sh' >/dev/null 2>&1; then
    echo "  🟡 autopilot-dispatch 正在跑"
  fi
  if pgrep -fl 'dispatch-cursor-agent-cli.sh' >/dev/null 2>&1; then
    echo "  🟢 cursor-agent dispatch 进行中"
  fi
  echo "  日志: $LOG_FILE"
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
  -h|--help)
    cat <<EOF
Usage: autopilot-launchagent-service.sh [install|uninstall|start|stop|restart|status|logs]

Preferred scheduler on macOS (GUI session → cursor-agent login works in nohup).
Replaces autopilot crontab lines on install.

Env: AUTOPILOT_LAUNCHAGENT_INTERVAL_SEC (default 1800)
EOF
    ;;
  *) echo "Unknown command: $1" >&2; exit 1 ;;
esac

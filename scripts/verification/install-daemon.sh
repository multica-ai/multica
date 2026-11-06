#!/usr/bin/env bash
# Installs the verification daemon as a user launchd service that runs 24/7.
# Agents connect to its fixed MCP address; the console is served at the root.
set -euo pipefail

verify_repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
verify_port="${MULTICA_VERIFICATION_PORT:-47311}"
verify_max="${MULTICA_VERIFICATION_MAX_ENVIRONMENTS:-2}"
verify_idle="${MULTICA_VERIFICATION_IDLE_MINUTES:-120}"
verify_bindings="${MULTICA_VERIFICATION_BINDINGS_FILE:-}"
verify_tools_root="${MULTICA_VERIFICATION_TOOLS_ROOT:-$HOME/.multica/verification-tools}"
verify_label="ai.multica.verification"
verify_plist="$HOME/Library/LaunchAgents/$verify_label.plist"
verify_log_dir="$HOME/.multica/verification/daemon"

bash "$verify_repo_root/scripts/verification/bootstrap.sh"
mkdir -p "$verify_log_dir" "$HOME/Library/LaunchAgents"
chmod 700 "$verify_log_dir"

xml() { printf '%s' "$1" | sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g'; }
# launchd starts services with a minimal PATH; keep the one that finds git,
# docker, go and make. Nothing else from this shell reaches the daemon.
verify_path="$verify_tools_root/node/bin:$PATH"

cat > "$verify_plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$verify_label</string>
  <key>ProgramArguments</key>
  <array>
    <string>$(xml "$verify_tools_root/node/bin/node")</string>
    <string>$(xml "$verify_repo_root/packages/verification/dist/daemon/main.js")</string>
    <string>--project</string><string>$(xml "$verify_repo_root")</string>
    <string>--port</string><string>$verify_port</string>
    <string>--max-environments</string><string>$verify_max</string>
    <string>--idle-minutes</string><string>$verify_idle</string>
$(if [ -n "$verify_bindings" ]; then printf '    <string>--bindings-file</string><string>%s</string>\n' "$(xml "$verify_bindings")"; fi)
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key><string>$(xml "$verify_path")</string>
    <key>HOME</key><string>$(xml "$HOME")</string>
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ProcessType</key><string>Interactive</string>
  <key>StandardOutPath</key><string>$(xml "$verify_log_dir/daemon.log")</string>
  <key>StandardErrorPath</key><string>$(xml "$verify_log_dir/daemon.log")</string>
</dict>
</plist>
PLIST

launchctl bootout "gui/$(id -u)/$verify_label" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$verify_plist"

for _ in $(seq 1 60); do
  curl -sf -o /dev/null "http://127.0.0.1:$verify_port/" && break
  sleep 1
done
curl -sf -o /dev/null "http://127.0.0.1:$verify_port/" \
  || { echo "The daemon did not start; see $verify_log_dir/daemon.log" >&2; exit 1; }

echo "Verification daemon is running."
echo "  MCP:     http://127.0.0.1:$verify_port/mcp"
echo "  Console: http://127.0.0.1:$verify_port/"
echo "Register it with an MCP client, for example:"
echo "  claude mcp add --scope user --transport http multica-verification http://127.0.0.1:$verify_port/mcp"

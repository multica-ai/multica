#!/usr/bin/env bash
# Stops every verification environment, then the daemon, and removes the
# launchd service. Environments otherwise survive daemon restarts, so they are
# stopped explicitly here. Caches and the repository mirror are kept.
set -euo pipefail
verify_label="ai.multica.verification"
verify_port="${MULTICA_VERIFICATION_PORT:-47311}"
verify_base="http://127.0.0.1:$verify_port"

if curl -sf -o /dev/null "$verify_base/"; then
  verify_ids="$(curl -sf "$verify_base/console/api/overview" | node -e '
    let text = ""; process.stdin.on("data", (c) => (text += c)).on("end", () => {
      const running = new Set(["preparing", "starting", "awaitingUser", "ready", "stopping", "interrupted", "failed"]);
      for (const e of JSON.parse(text).environments)
        if (running.has(e.environment.state)) console.log(e.environment.environmentId);
    });')"
  for verify_id in $verify_ids; do
    curl -sf -X POST -H "Origin: $verify_base" "$verify_base/console/api/environments/$verify_id/stop" >/dev/null || true
  done
  for _ in $(seq 1 120); do
    verify_left="$(curl -sf "$verify_base/console/api/overview" | node -e '
      let text = ""; process.stdin.on("data", (c) => (text += c)).on("end", () => {
        console.log(JSON.parse(text).environments.filter((e) => e.environment.state !== "stopped").length);
      });')"
    [ "$verify_left" = "0" ] && break
    sleep 2
  done
fi

launchctl bootout "gui/$(id -u)/$verify_label" 2>/dev/null || true
rm -f "$HOME/Library/LaunchAgents/$verify_label.plist"
echo "Verification daemon removed."

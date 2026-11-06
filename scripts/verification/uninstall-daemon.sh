#!/usr/bin/env bash
# Stops the verification daemon (it stops and cleans up its environments) and
# removes the launchd service. Caches and the repository mirror are kept.
set -euo pipefail
verify_label="ai.multica.verification"
launchctl bootout "gui/$(id -u)/$verify_label" 2>/dev/null || true
rm -f "$HOME/Library/LaunchAgents/$verify_label.plist"
echo "Verification daemon removed."

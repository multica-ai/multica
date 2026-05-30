#!/usr/bin/env bash
# packaging/launchd/install.sh — install or uninstall the multica-token-sync
# launchd agent on macOS. Run as the operator (no sudo).

set -euo pipefail

CMD="${1:-install}"
LABEL="com.multica.token-sync"
PLIST_SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/${LABEL}.plist"
PLIST_DST="$HOME/Library/LaunchAgents/${LABEL}.plist"
BIN_DST="/usr/local/bin/multica-token-sync"

ensure_binary() {
  if [[ ! -x "$BIN_DST" ]]; then
    echo "error: $BIN_DST not found or not executable" >&2
    echo "Build with: cd server && go build -o /tmp/multica-token-sync ./cmd/multica-token-sync" >&2
    echo "Install with: sudo install -m 0755 /tmp/multica-token-sync $BIN_DST" >&2
    exit 1
  fi
}

case "$CMD" in
  install)
    ensure_binary
    mkdir -p "$HOME/Library/Logs" "$HOME/Library/LaunchAgents"
    sed "s|__USER_HOME__|$HOME|g" "$PLIST_SRC" > "$PLIST_DST"
    launchctl bootout "gui/$(id -u)/${LABEL}" 2>/dev/null || true
    launchctl bootstrap "gui/$(id -u)" "$PLIST_DST"
    echo "Installed at $PLIST_DST"
    echo "Logs: $HOME/Library/Logs/multica-token-sync.log"
    echo "Status: $0 status"
    ;;
  uninstall)
    launchctl bootout "gui/$(id -u)/${LABEL}" 2>/dev/null || true
    rm -f "$PLIST_DST"
    echo "Uninstalled."
    ;;
  status)
    launchctl print "gui/$(id -u)/${LABEL}" 2>&1 | head -30
    ;;
  *)
    echo "usage: $0 [install|uninstall|status]" >&2
    exit 2
    ;;
esac

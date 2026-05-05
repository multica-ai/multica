#!/usr/bin/env bash
# CEREBRO-PATCH(install-runtime-script): cerebro modification of upstream file
# Multica runtime installer — fetches the multica CLI, configures the daemon,
# and starts it as a background service. Triggered by the one-line command
# shown in the "Add runtime" dialog:
#
#   curl -fsSL <server>/install-runtime.sh | bash -s -- --token <setup-token>
#
# The setup token is exchanged once for a long-lived PAT plus workspace config.
set -euo pipefail

SERVER_URL="__SERVER_URL__"
TOKEN=""
DEVICE_LABEL=""
ASSUME_YES=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --token)        TOKEN="$2"; shift 2 ;;
    --device-label) DEVICE_LABEL="$2"; shift 2 ;;
    --server-url)   SERVER_URL="$2"; shift 2 ;;
    --yes|-y)       ASSUME_YES=true; shift ;;
    *) echo "unknown arg: $1" >&2; exit 1 ;;
  esac
done

if [[ -z "$TOKEN" ]]; then
  echo "error: --token is required" >&2
  exit 1
fi

if [[ -z "$DEVICE_LABEL" ]]; then
  DEVICE_LABEL="$(scutil --get ComputerName 2>/dev/null || hostname)"
fi

OS="$(uname -s)"
case "$OS" in
  Darwin) OS=mac ;;
  Linux)  OS=linux ;;
  *) echo "unsupported OS: $OS" >&2; exit 1 ;;
esac

CONFIG_DIR="$HOME/.multica"
PROFILE="local"
PROFILE_DIR="$CONFIG_DIR/profiles/$PROFILE"
mkdir -p "$PROFILE_DIR"

step() { printf "\033[1;36m▸\033[0m %s\n" "$1"; }
ok()   { printf "\033[1;32m✓\033[0m %s\n" "$1"; }
warn() { printf "\033[1;33m!\033[0m %s\n" "$1"; }

# 1. Install multica CLI if missing.
# Pull the pre-built binary straight from GitHub Releases. We deliberately
# avoid Homebrew (and the upstream `scripts/install.sh` that wraps brew) —
# they trigger a source build that needs Xcode Command Line Tools, which is
# a painful prerequisite for non-developers.
if ! command -v multica >/dev/null 2>&1; then
  step "Installing multica CLI (GitHub release binary)…"

  ARCH="$(uname -m)"
  case "$ARCH" in
    arm64|aarch64) ARCH=arm64 ;;
    x86_64|amd64)  ARCH=amd64 ;;
    *) echo "error: unsupported arch $ARCH" >&2; exit 1 ;;
  esac
  RELEASE_OS="$OS"
  [[ "$RELEASE_OS" == "mac" ]] && RELEASE_OS=darwin

  TAG=$(curl -fsSL "https://api.github.com/repos/multica-ai/multica/releases/latest" \
    | sed -nE 's/.*"tag_name":[[:space:]]*"([^"]+)".*/\1/p' | head -1)
  if [[ -z "$TAG" ]]; then
    echo "error: could not read latest multica release tag" >&2
    exit 1
  fi
  VER="${TAG#v}"
  ASSET="multica-cli-${VER}-${RELEASE_OS}-${ARCH}.tar.gz"
  URL="https://github.com/multica-ai/multica/releases/download/${TAG}/${ASSET}"

  TMP="$(mktemp -d)"
  trap 'rm -rf "$TMP"' EXIT
  curl -fsSL "$URL" -o "$TMP/multica.tar.gz"
  tar -xzf "$TMP/multica.tar.gz" -C "$TMP"

  # The tarball contains a binary named `multica` somewhere — find it.
  BIN_PATH="$(find "$TMP" -type f -name multica -perm -u+x | head -1)"
  if [[ -z "$BIN_PATH" ]]; then
    BIN_PATH="$(find "$TMP" -type f -name multica | head -1)"
  fi
  if [[ -z "$BIN_PATH" ]]; then
    echo "error: multica binary not found inside $ASSET" >&2
    exit 1
  fi
  chmod +x "$BIN_PATH"

  # Pick a writable install location: prefer a system path if writable,
  # otherwise fall back to a user-local path and prepend it to the user's PATH.
  INSTALL_DIR=""
  for candidate in /usr/local/bin /opt/homebrew/bin "$HOME/.local/bin"; do
    if [[ -w "$candidate" ]] || ([[ ! -e "$candidate" ]] && mkdir -p "$candidate" 2>/dev/null && [[ -w "$candidate" ]]); then
      INSTALL_DIR="$candidate"
      break
    fi
  done
  if [[ -z "$INSTALL_DIR" ]]; then
    INSTALL_DIR="$HOME/.local/bin"
    mkdir -p "$INSTALL_DIR"
  fi
  install -m 0755 "$BIN_PATH" "$INSTALL_DIR/multica"
  # Strip macOS quarantine so launchd can exec the binary without a Gatekeeper popup.
  [[ "$OS" == "mac" ]] && xattr -d com.apple.quarantine "$INSTALL_DIR/multica" 2>/dev/null || true

  if ! echo ":$PATH:" | grep -q ":$INSTALL_DIR:"; then
    export PATH="$INSTALL_DIR:$PATH"
    SHELL_RC="$HOME/.zshrc"
    [[ -n "${BASH_VERSION:-}" ]] && SHELL_RC="$HOME/.bashrc"
    echo "" >> "$SHELL_RC"
    echo "# Added by multica installer on $(date -Iseconds)" >> "$SHELL_RC"
    echo "export PATH=\"$INSTALL_DIR:\$PATH\"" >> "$SHELL_RC"
    warn "Added $INSTALL_DIR to PATH in $SHELL_RC — open a new shell or 'source $SHELL_RC' to pick it up"
  fi
  hash -r 2>/dev/null || true

  if ! command -v multica >/dev/null 2>&1; then
    echo "error: multica still not on PATH after install" >&2
    exit 1
  fi
  ok "multica CLI installed at $(command -v multica) ($TAG)"
else
  ok "multica CLI already installed at $(command -v multica)"
fi

# 2. Exchange setup token for daemon credentials.
step "Exchanging setup token for daemon credentials…"
EXCHANGE_BODY=$(cat <<JSON
{"token":"$TOKEN","device_label":"$DEVICE_LABEL"}
JSON
)
RESPONSE=$(curl -fsSL -X POST "$SERVER_URL/api/runtime-setup/exchange" \
  -H "Content-Type: application/json" \
  -d "$EXCHANGE_BODY")

extract() { printf '%s' "$RESPONSE" | sed -nE "s/.*\"$1\":\"([^\"]+)\".*/\1/p"; }
DAEMON_TOKEN=$(extract daemon_token)
WORKSPACE_ID=$(extract workspace_id)
WORKSPACE_SLUG=$(extract workspace_slug)
WORKSPACE_NAME=$(extract workspace_name)
APP_URL=$(extract app_url)

if [[ -z "$DAEMON_TOKEN" || -z "$WORKSPACE_ID" ]]; then
  echo "error: exchange failed — response: $RESPONSE" >&2
  exit 1
fi
ok "Linked to workspace: $WORKSPACE_NAME ($WORKSPACE_SLUG)"

# 3. Write CLI config.
# The daemon runs with `--profile local`, which reads profile-scoped config
# from ~/.multica/profiles/local/config.json. We mirror to the root path too
# so plain `multica` invocations (without --profile) pick up the same auth.
step "Writing CLI config to $PROFILE_DIR/config.json"
CONFIG_BODY=$(cat <<JSON
{
  "server_url": "$SERVER_URL",
  "app_url": "$APP_URL",
  "token": "$DAEMON_TOKEN",
  "workspace_id": "$WORKSPACE_ID",
  "watched_workspaces": [
    { "id": "$WORKSPACE_ID", "name": "$WORKSPACE_NAME" }
  ]
}
JSON
)
printf '%s\n' "$CONFIG_BODY" > "$PROFILE_DIR/config.json"
chmod 600 "$PROFILE_DIR/config.json"
printf '%s\n' "$CONFIG_BODY" > "$CONFIG_DIR/config.json"
chmod 600 "$CONFIG_DIR/config.json"
ok "Config written"

# 4. Start daemon (and install autostart on macOS).
if [[ "$OS" == "mac" ]]; then
  PLIST="$HOME/Library/LaunchAgents/com.multica.daemon.plist"
  step "Installing launchd autostart at $PLIST"
  MULTICA_BIN="$(command -v multica)"
  cat > "$PLIST" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.multica.daemon</string>
  <key>ProgramArguments</key>
  <array>
    <string>$MULTICA_BIN</string>
    <string>daemon</string>
    <string>start</string>
    <string>--profile</string><string>local</string>
    <string>--foreground</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>HOME</key><string>$HOME</string>
    <key>PATH</key><string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string>
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>$CONFIG_DIR/daemon.out.log</string>
  <key>StandardErrorPath</key><string>$CONFIG_DIR/daemon.err.log</string>
  <key>ProcessType</key><string>Background</string>
</dict>
</plist>
PLIST

  UID_NUM="$(id -u)"
  launchctl bootout "gui/$UID_NUM/com.multica.daemon" 2>/dev/null || true
  launchctl bootstrap "gui/$UID_NUM" "$PLIST"
  ok "launchd job loaded — daemon will autostart at login"
else
  step "Starting daemon (Linux: no autostart wired up — run from a service manager)"
  multica daemon start --profile local || true
fi

# 5. Smoke check: status.
sleep 2
multica daemon status || warn "daemon status check failed — inspect $CONFIG_DIR/daemon.err.log"

ok "Done. Open $APP_URL/$WORKSPACE_SLUG/runtimes to see this device come online."

#!/usr/bin/env bash
# Multica installer — installs the CLI and optionally provisions a self-host server.
#
# Install / upgrade CLI only:
#   curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash
#
# Install CLI + provision self-host server:
#   curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash -s -- --with-server
#
# The default install flow configures the internal cloud, opens browser login,
# and starts the daemon automatically.
#
set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
REPO_URL="https://github.com/multica-ai/multica.git"
INSTALL_DIR="${MULTICA_INSTALL_DIR:-$HOME/.multica/server}"
CLI_BIN_DIR="${MULTICA_CLI_BIN_DIR:-$HOME/.multica/bin}"
CLI_BIN_PATH="$CLI_BIN_DIR/multica"
APP_URL="${MULTICA_APP_URL:-https://multica.wujieai.com}"
SERVER_URL="${MULTICA_SERVER_URL:-https://multica.wujieai.com}"
UPDATE_MANIFEST_URL="${MULTICA_UPDATE_MANIFEST_URL:-https://mock-oss.multica.local/cli/manifest.json}"

# Colors (disabled when not a terminal)
if [ -t 1 ] || [ -t 2 ]; then
  BOLD='\033[1m'
  GREEN='\033[0;32m'
  YELLOW='\033[0;33m'
  RED='\033[0;31m'
  CYAN='\033[0;36m'
  RESET='\033[0m'
else
  BOLD='' GREEN='' YELLOW='' RED='' CYAN='' RESET=''
fi

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
info()  { printf "${BOLD}${CYAN}==> %s${RESET}\n" "$*"; }
ok()    { printf "${BOLD}${GREEN}✓ %s${RESET}\n" "$*"; }
warn()  { printf "${BOLD}${YELLOW}⚠ %s${RESET}\n" "$*" >&2; }
fail()  { printf "${BOLD}${RED}✗ %s${RESET}\n" "$*" >&2; exit 1; }

command_exists() { command -v "$1" >/dev/null 2>&1; }

json_get() {
  local expr="$1"
  python3 -c 'import json,sys; data=json.load(sys.stdin); expr=sys.argv[1]; cur=data
for part in expr.split("."):
    if not part:
        continue
    if "[" in part and part.endswith("]"):
        name, idx = part[:-1].split("[", 1)
        if name:
            cur = cur[name]
        cur = cur[int(idx)]
    else:
        cur = cur[part]
if isinstance(cur, bool):
    print("true" if cur else "false")
elif cur is None:
    print("")
else:
    print(cur)' "$expr"
}

detect_os() {
  case "$(uname -s)" in
    Darwin) OS="darwin" ;;
    Linux)  OS="linux" ;;
    MINGW*|MSYS*|CYGWIN*)
            fail "This script does not support Windows. Use the PowerShell installer instead:
  irm https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.ps1 | iex" ;;
    *)      fail "Unsupported operating system: $(uname -s). Multica supports macOS, Linux, and Windows." ;;
  esac

  ARCH="$(uname -m)"
  case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    arm64)   ARCH="arm64" ;;
    *)       fail "Unsupported architecture: $ARCH" ;;
  esac
}

# ---------------------------------------------------------------------------
# CLI Installation
# ---------------------------------------------------------------------------
find_manifest_asset_index() {
  local manifest="$1"
  MANIFEST_PATH="$manifest" python3 - "$OS" "$ARCH" <<'PY'
import json, os, sys
with open(os.environ["MANIFEST_PATH"], "r", encoding="utf-8") as fh:
    data = json.load(fh)
target_os, target_arch = sys.argv[1], sys.argv[2]
for idx, asset in enumerate(data.get("assets", [])):
    if asset.get("os") == target_os and asset.get("arch") == target_arch:
        print(idx)
        break
else:
    sys.exit(1)
PY
}

install_cli_binary() {
  info "Installing Multica CLI from update manifest..."

  local tmp_dir manifest_path asset_index asset_url asset_checksum asset_version archive_path
  tmp_dir=$(mktemp -d)
  manifest_path="$tmp_dir/manifest.json"

  if ! curl -fsSL "$UPDATE_MANIFEST_URL" -o "$manifest_path"; then
    rm -rf "$tmp_dir"
    fail "Failed to download update manifest from $UPDATE_MANIFEST_URL"
  fi

  if ! asset_index=$(find_manifest_asset_index "$manifest_path"); then
    rm -rf "$tmp_dir"
    fail "No matching asset in manifest for $OS/$ARCH"
  fi

  asset_url=$(json_get "assets[$asset_index].download_url" < "$manifest_path")
  asset_checksum=$(json_get "assets[$asset_index].checksum" < "$manifest_path")
  asset_version=$(json_get "version" < "$manifest_path")
  archive_path="$tmp_dir/archive.$(release_archive_extension)"

  info "Downloading ${asset_version} from $asset_url ..."
  if ! curl -fsSL "$asset_url" -o "$archive_path"; then
    rm -rf "$tmp_dir"
    fail "Failed to download CLI archive."
  fi

  local actual_checksum
  actual_checksum=$(shasum -a 256 "$archive_path" | awk '{print $1}')
  if [ "$actual_checksum" != "$asset_checksum" ]; then
    rm -rf "$tmp_dir"
    fail "Checksum verification failed. Expected $asset_checksum, got $actual_checksum"
  fi

  mkdir -p "$CLI_BIN_DIR"
  tar -xzf "$archive_path" -C "$tmp_dir" multica
  mv "$tmp_dir/multica" "$CLI_BIN_PATH"
  chmod +x "$CLI_BIN_PATH"

  if ! echo "$PATH" | tr ':' '\n' | grep -q "^$CLI_BIN_DIR$"; then
    export PATH="$CLI_BIN_DIR:$PATH"
    add_to_path "$CLI_BIN_DIR"
  fi

  rm -rf "$tmp_dir"
  ok "Multica CLI installed to $CLI_BIN_PATH"
}

release_archive_extension() {
  if [ "$OS" = "windows" ]; then
    printf "zip"
  else
    printf "tar.gz"
  fi
}

add_to_path() {
  local dir="$1"
  local line="export PATH=\"$dir:\$PATH\""
  for rc in "$HOME/.bashrc" "$HOME/.zshrc"; do
    if [ -f "$rc" ] && ! grep -qF "$dir" "$rc"; then
      printf '\n# Added by Multica installer\n%s\n' "$line" >> "$rc"
    fi
  done
}

get_latest_version() {
  local tmp_dir manifest_path
  tmp_dir=$(mktemp -d)
  manifest_path="$tmp_dir/manifest.json"
  if ! curl -fsSL "$UPDATE_MANIFEST_URL" -o "$manifest_path" 2>/dev/null; then
    rm -rf "$tmp_dir"
    return 0
  fi
  json_get "version" < "$manifest_path" || true
  rm -rf "$tmp_dir"
}

install_cli() {
  if command_exists multica && [ "$(command -v multica)" != "$CLI_BIN_PATH" ]; then
    warn "Detected another multica on PATH at $(command -v multica). The managed install will use $CLI_BIN_PATH."
  fi

  if [ -x "$CLI_BIN_PATH" ]; then
    local current_ver
    current_ver=$("$CLI_BIN_PATH" version 2>/dev/null | awk '{print $2}' || echo "unknown")

    local latest_ver
    latest_ver=$(get_latest_version)

    # Normalize: strip leading 'v' for comparison
    local current_cmp="${current_ver#v}"
    local latest_cmp="${latest_ver#v}"

    if [ -z "$latest_ver" ] || [ "$current_cmp" = "$latest_cmp" ]; then
      ok "Multica CLI is up to date ($current_ver)"
      return 0
    fi

    info "Multica CLI $current_ver installed, latest is $latest_ver — upgrading..."
    install_cli_binary

    local new_ver
    new_ver=$("$CLI_BIN_PATH" version 2>/dev/null | awk '{print $2}' || echo "unknown")
    ok "Multica CLI upgraded ($current_ver → $new_ver)"
    return 0
  fi

  install_cli_binary

  # Verify
  if [ ! -x "$CLI_BIN_PATH" ]; then
    fail "CLI installed but 'multica' not found on PATH. You may need to restart your shell."
  fi
}

configure_internal_cloud() {
  info "Configuring Multica CLI for $APP_URL ..."
  "$CLI_BIN_PATH" config set server_url "$SERVER_URL" >/dev/null
  "$CLI_BIN_PATH" config set app_url "$APP_URL" >/dev/null
  "$CLI_BIN_PATH" config set update_manifest_url "$UPDATE_MANIFEST_URL" >/dev/null
  ok "CLI config updated for the internal cloud"
}

is_daemon_running() {
  "$CLI_BIN_PATH" daemon status 2>/dev/null | grep -qi "running"
}

login_and_start_daemon() {
  printf "\n"
  info "Opening browser login for $APP_URL ..."
  info "Complete authorization in the browser, then return here."
  if ! "$CLI_BIN_PATH" login; then
    fail "Login did not complete successfully."
  fi

  if is_daemon_running; then
    ok "Multica daemon is already running"
    return 0
  fi

  info "Starting Multica daemon..."
  if ! "$CLI_BIN_PATH" daemon start; then
    fail "Failed to start the Multica daemon."
  fi
  ok "Multica daemon started"
}

# ---------------------------------------------------------------------------
# Docker check
# ---------------------------------------------------------------------------
check_docker() {
  if ! command_exists docker; then
    printf "\n"
    fail "Docker is not installed. Multica self-hosting requires Docker and Docker Compose.

Install Docker:
  macOS:  https://docs.docker.com/desktop/install/mac-install/
  Linux:  https://docs.docker.com/engine/install/

After installing Docker, re-run this script with --with-server."
  fi

  if ! docker info >/dev/null 2>&1; then
    fail "Docker is installed but not running. Please start Docker and re-run this script."
  fi

  ok "Docker is available"
}

# ---------------------------------------------------------------------------
# Server setup (self-host / --with-server)
# ---------------------------------------------------------------------------
setup_server() {
  info "Setting up Multica server..."

  if [ -d "$INSTALL_DIR/.git" ]; then
    info "Updating existing installation at $INSTALL_DIR..."
    cd "$INSTALL_DIR"
    git fetch origin main --depth 1 2>/dev/null || true
    git reset --hard origin/main 2>/dev/null || true
  else
    info "Cloning Multica repository..."
    if ! command_exists git; then
      fail "Git is not installed. Please install git and re-run."
    fi
    # Remove leftover directory from a previously interrupted clone
    if [ -d "$INSTALL_DIR" ]; then
      warn "Removing incomplete installation at $INSTALL_DIR..."
      rm -rf "$INSTALL_DIR"
    fi
    mkdir -p "$(dirname "$INSTALL_DIR")"
    git clone --depth 1 "$REPO_URL" "$INSTALL_DIR"
    cd "$INSTALL_DIR"
  fi

  ok "Repository ready at $INSTALL_DIR"

  # Generate .env if needed
  if [ ! -f .env ]; then
    info "Creating .env with random JWT_SECRET..."
    cp .env.example .env
    local jwt
    jwt=$(openssl rand -hex 32)
    if [ "$(uname -s)" = "Darwin" ]; then
      sed -i '' "s/^JWT_SECRET=.*/JWT_SECRET=$jwt/" .env
    else
      sed -i "s/^JWT_SECRET=.*/JWT_SECRET=$jwt/" .env
    fi
    ok "Generated .env with random JWT_SECRET"
  else
    ok "Using existing .env"
  fi

  # Start Docker Compose
  info "Starting Multica services (this may take a few minutes on first run)..."
  docker compose -f docker-compose.selfhost.yml up -d --build

  # Wait for health check
  info "Waiting for backend to be ready..."
  local ready=false
  for i in $(seq 1 45); do
    if curl -sf http://localhost:8080/health >/dev/null 2>&1; then
      ready=true
      break
    fi
    sleep 2
  done

  if [ "$ready" = true ]; then
    ok "Multica server is running"
  else
    warn "Server is still starting. You can check logs with:"
    echo "  cd $INSTALL_DIR && docker compose -f docker-compose.selfhost.yml logs"
    echo ""
  fi
}


# ---------------------------------------------------------------------------
# Main: Default mode (install / upgrade CLI only)
# ---------------------------------------------------------------------------
run_default() {
  printf "\n"
  printf "${BOLD}  Multica — Installer${RESET}\n"
  printf "  Configuring the internal cloud at ${CYAN}%s${RESET}\n" "$APP_URL"
  printf "\n"

  detect_os
  install_cli
  configure_internal_cloud
  login_and_start_daemon

  printf "\n"
  printf "${BOLD}${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}\n"
  printf "${BOLD}${GREEN}  ✓ Multica CLI is ready!${RESET}\n"
  printf "${BOLD}${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}\n"
  printf "\n"
  printf "  ${BOLD}Configured server:${RESET} %s\n" "$SERVER_URL"
  printf "  ${BOLD}Configured app:${RESET}    %s\n" "$APP_URL"
  printf "\n"
  printf "     ${CYAN}multica config list${RESET}          # Verify config values\n"
  printf "     ${CYAN}multica daemon status${RESET}        # Verify daemon status\n"
  printf "\n"
  printf "  ${BOLD}Self-hosting?${RESET} Install the server first:\n"
  printf "     curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash -s -- --with-server\n"
  printf "\n"
}

# ---------------------------------------------------------------------------
# Main: With-server mode (provision self-host infrastructure + install CLI)
# ---------------------------------------------------------------------------
run_with_server() {
  printf "\n"
  printf "${BOLD}  Multica — Self-Host Installer${RESET}\n"
  printf "  Provisioning server infrastructure + installing CLI\n"
  printf "\n"

  detect_os
  check_docker
  setup_server
  install_cli

  printf "\n"
  printf "${BOLD}${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}\n"
  printf "${BOLD}${GREEN}  ✓ Multica server is running and CLI is ready!${RESET}\n"
  printf "${BOLD}${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}\n"
  printf "\n"
  printf "  ${BOLD}Frontend:${RESET}  http://localhost:3000\n"
  printf "  ${BOLD}Backend:${RESET}   http://localhost:8080\n"
  printf "  ${BOLD}Server at:${RESET} %s\n" "$INSTALL_DIR"
  printf "\n"
  printf "  ${BOLD}Next: configure your CLI to connect${RESET}\n"
  printf "\n"
  printf "     ${CYAN}multica setup self-host${RESET}   # Configure + authenticate + start daemon\n"
  printf "\n"
  printf "  ${BOLD}Login:${RESET} configure ${CYAN}RESEND_API_KEY${RESET} in .env for email codes,\n"
  printf "  or set ${CYAN}APP_ENV=development${RESET} in .env to enable the dev master code ${BOLD}888888${RESET}.\n"
  printf "\n"
  printf "  ${BOLD}To stop all services:${RESET}\n"
  printf "     curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash -s -- --stop\n"
  printf "\n"
}

# ---------------------------------------------------------------------------
# Stop: shut down a self-hosted installation
# ---------------------------------------------------------------------------
run_stop() {
  printf "\n"
  info "Stopping Multica services..."

  if [ -d "$INSTALL_DIR" ]; then
    cd "$INSTALL_DIR"
    if [ -f docker-compose.selfhost.yml ]; then
      docker compose -f docker-compose.selfhost.yml down
      ok "Docker services stopped"
    else
      warn "No docker-compose.selfhost.yml found at $INSTALL_DIR"
    fi
  else
    warn "No Multica installation found at $INSTALL_DIR"
  fi

  if command_exists multica; then
    multica daemon stop 2>/dev/null && ok "Daemon stopped" || true
  fi

  printf "\n"
}

# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------
main() {
  local mode="default"

  while [ $# -gt 0 ]; do
    case "$1" in
      --with-server) mode="with-server" ;;
      --local)       mode="with-server" ;;  # backwards compat alias
      --stop)        mode="stop" ;;
      --help|-h)
        echo "Usage: install.sh [--with-server | --stop]"
        echo ""
        echo "  (default)       Install / upgrade the Multica CLI, configure the internal cloud,"
        echo "                  run browser login, and start the daemon"
        echo "  --with-server   Install CLI + provision a self-host server (Docker)"
        echo "  --stop          Stop a self-hosted installation"
        echo ""
        exit 0
        ;;
      *) warn "Unknown option: $1" ;;
    esac
    shift
  done

  case "$mode" in
    default)     run_default ;;
    with-server) run_with_server ;;
    stop)        run_stop ;;
  esac
}

main "$@"

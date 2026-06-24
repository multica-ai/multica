#!/usr/bin/env bash
# Multica installer — installs the CLI and optionally provisions a self-host server.
#
# SYNC NOTICE: This file shares login/daemon logic with
# server/internal/handler/install_scripts/install.sh (the embed version served
# at multica.wujieai.com/install.sh). Both must default APP_URL/SERVER_URL to
# multica.wujieai.com and include login_and_start_daemon().
# Run `make sync-install-scripts` to verify invariants.
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
BREW_PACKAGE="multica-ai/tap/multica"
APP_URL="${MULTICA_APP_URL:-https://multica.wujieai.com}"
SERVER_URL="${MULTICA_SERVER_URL:-https://multica.wujieai.com}"
UPDATE_MANIFEST_URL="${MULTICA_UPDATE_MANIFEST_URL:-https://multica.obs.cn-east-3.myhuaweicloud.com/cli/manifest.json}"

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

paths_match() {
  python3 - "$1" "$2" <<'PY'
import os, sys
a, b = sys.argv[1], sys.argv[2]
if not (os.path.exists(a) and os.path.exists(b)):
    raise SystemExit(1)
raise SystemExit(0 if os.path.realpath(a) == os.path.realpath(b) else 1)
PY
}

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
managed_install_marker_path() {
  printf '%s\n' "$CLI_BIN_DIR/.install-source.json"
}

is_managed_install() {
  local marker_path install_channel
  [ -x "$CLI_BIN_PATH" ] || return 1

  marker_path=$(managed_install_marker_path)
  [ -f "$marker_path" ] || return 1

  install_channel=$(json_get "install_channel" < "$marker_path" 2>/dev/null || true)
  [ "$install_channel" = "managed-manifest" ]
}

write_managed_install_marker() {
  local marker_path installed_at
  marker_path=$(managed_install_marker_path)
  installed_at=$(date -u '+%Y-%m-%dT%H:%M:%SZ')

  python3 - "$marker_path" "$UPDATE_MANIFEST_URL" "$installed_at" <<'PY'
import json, sys
path, manifest_url, installed_at = sys.argv[1:4]
data = {
    "install_channel": "managed-manifest",
    "installed_at": installed_at,
    "manifest_url": manifest_url,
    "installer_version": "managed-manifest-v1",
}
with open(path, "w", encoding="utf-8") as fh:
    json.dump(data, fh, ensure_ascii=True, indent=2)
    fh.write("\n")
PY
}

legacy_install_candidates_unix() {
  if command_exists brew && brew list "$BREW_PACKAGE" >/dev/null 2>&1; then
    printf 'brew\n'
  fi

  local candidate
  for candidate in "/usr/local/bin/multica" "$HOME/.local/bin/multica"; do
    [ -e "$candidate" ] || continue
    if paths_match "$candidate" "$CLI_BIN_PATH"; then
      continue
    fi
    printf 'path:%s\n' "$candidate"
  done
}

has_legacy_install_unix() {
  local candidates
  candidates=$(legacy_install_candidates_unix || true)
  [ -n "$candidates" ]
}

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
  local staged_binary backup_path had_existing_cli=0
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
  staged_binary="$tmp_dir/multica"
  chmod +x "$staged_binary"

  backup_path="$CLI_BIN_PATH.bak"
  rm -f "$backup_path"
  if [ -e "$CLI_BIN_PATH" ]; then
    had_existing_cli=1
    stop_managed_daemon_for_replace
    if ! mv "$CLI_BIN_PATH" "$backup_path"; then
      rm -rf "$tmp_dir"
      fail "Failed to move existing CLI out of the way before installing the new version."
    fi
  fi

  if mv "$staged_binary" "$CLI_BIN_PATH"; then
    rm -f "$backup_path"
    # macOS: ad-hoc sign to prevent Gatekeeper from killing the binary
    if [ "$(uname -s)" = "Darwin" ]; then
      codesign --force --sign - "$CLI_BIN_PATH" 2>/dev/null || true
    fi
  else
    if [ "$had_existing_cli" -eq 1 ] && [ -e "$backup_path" ]; then
      mv "$backup_path" "$CLI_BIN_PATH" || true
    fi
    rm -rf "$tmp_dir"
    fail "Failed to install the new CLI binary."
  fi

  if ! write_managed_install_marker; then
    rm -rf "$tmp_dir"
    fail "Installed the CLI but failed to write the managed install marker."
  fi

  # Persist CLI_BIN_DIR on PATH for future terminals (idempotent).
  add_to_path "$CLI_BIN_DIR"
  # Make 'multica' usable in THIS terminal immediately. curl|sh runs in a
  # subshell that cannot mutate the parent's PATH, so we symlink the managed
  # binary into a directory already on PATH instead of relying on `source`.
  link_multica_into_path

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

# Find the first directory that is already on PATH and writable by us.
# Used so a `curl | sh` subshell can make `multica` resolvable in the parent
# shell without asking the user to `source` anything (a subshell cannot mutate
# the parent's PATH env var).
find_writable_path_dir() {
  local dir
  local IFS=':'
  for dir in $PATH; do
    [ -n "$dir" ] || continue
    [ -d "$dir" ] || continue
    [ -w "$dir" ] || continue
    # Skip the managed dir itself — it is precisely what is missing from PATH.
    [ "$dir" = "$CLI_BIN_DIR" ] && continue
    printf '%s\n' "$dir"
    return 0
  done
  return 1
}

# Make `multica` usable in the CURRENT terminal immediately after install.
# curl|sh runs in a subshell; exporting PATH here only affects the subshell.
# Instead we symlink the managed binary into a directory already on PATH.
link_multica_into_path() {
  local target="$CLI_BIN_PATH"

  # Already resolvable (reinstall, or CLI_BIN_DIR already on PATH)?
  if command -v multica >/dev/null 2>&1; then
    return 0
  fi

  local dir
  dir=$(find_writable_path_dir) || true
  if [ -n "$dir" ]; then
    if ln -sf "$target" "$dir/multica" 2>/dev/null; then
      if command -v multica >/dev/null 2>&1; then
        ok "Linked multica -> $dir/multica (usable in this terminal now)"
        return 0
      fi
    fi
  fi

  # No writable PATH dir without sudo: offer a system-wide symlink.
  if [ -d /usr/local/bin ] && command -v sudo >/dev/null 2>&1; then
    printf "\n"
    warn "No writable directory found on your PATH."
    warn "Install a symlink into /usr/local/bin so 'multica' works immediately?"
    printf "    This will run: sudo ln -sf %s /usr/local/bin/multica\n" "$target"
    printf "    Proceed? [y/N] "
    local reply
    read -r reply </dev/tty 2>/dev/null || reply=""
    case "$reply" in
      y|Y|yes|YES)
        if sudo ln -sf "$target" /usr/local/bin/multica; then
          if command -v multica >/dev/null 2>&1; then
            ok "Linked multica -> /usr/local/bin/multica (usable in this terminal now)"
            return 0
          fi
        fi
        ;;
    esac
  fi

  # Last resort: same-shell usability is physically impossible here (subshell
  # can't mutate parent PATH and no writable PATH dir exists). add_to_path has
  # already persisted the entry for future terminals.
  warn "Could not make 'multica' available in this terminal automatically."
  warn "Open a new terminal, or run: export PATH=\"$CLI_BIN_DIR:\$PATH\""
}

get_selfhost_ref() {
  if [ -n "${MULTICA_SELFHOST_REF:-}" ]; then
    printf '%s' "$MULTICA_SELFHOST_REF"
    return
  fi

  local latest
  latest=$(get_latest_version)
  if [ -n "$latest" ]; then
    printf '%s' "$latest"
    return
  fi

  printf '%s' "main"
}

checkout_server_ref() {
  local ref="$1"

  if [ "$ref" = "main" ]; then
    git fetch origin main --depth 1 2>/dev/null || true
    git checkout --force main 2>/dev/null || true
    git reset --hard origin/main 2>/dev/null || true
    return
  fi

  git fetch origin --tags --force 2>/dev/null || true
  if git rev-parse --verify --quiet "refs/tags/$ref" >/dev/null; then
    git checkout --force "$ref" 2>/dev/null || git checkout --force "tags/$ref" 2>/dev/null || true
    return
  fi

  git fetch origin "$ref" --depth 1 2>/dev/null || true
  git checkout --force "$ref" 2>/dev/null || true
}

pull_official_selfhost_images() {
  if docker compose -f docker-compose.selfhost.yml pull; then
    return
  fi

  echo ""
  warn "Official images for the selected self-host channel are not published yet."
  echo "This can happen before the first GHCR release is available."
  echo "From $INSTALL_DIR, build from source instead:"
  echo "  docker compose -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml up -d --build"
  exit 1
}

upgrade_cli_brew() {
  info "Upgrading Multica CLI via Homebrew..."
  brew update 2>/dev/null || true
  if brew upgrade "$BREW_PACKAGE" 2>/dev/null; then
    ok "Multica CLI upgraded via Homebrew"
  else
    # brew upgrade exits non-zero if already up to date
    ok "Multica CLI is already the latest version"
  fi
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

cli_version_parts() {
  local version="$1"
  VERSION="$version" python3 <<'PY'
import os
import re
import sys

version = os.environ.get("VERSION", "").strip()
match = re.fullmatch(r"v?(\d+)\.(\d+)\.(\d+)(?:-(\d+)(?:-[0-9A-Za-z.-]+)?)?", version)
if not match:
    sys.exit(1)
major, minor, patch, commits = match.groups()
print(f"{int(major)} {int(minor)} {int(patch)} {int(commits or 0)}")
PY
}

is_cli_version_at_least() {
  local current="$1"
  local latest="$2"
  local current_parts latest_parts

  current_parts=$(cli_version_parts "$current" 2>/dev/null) || return 1
  latest_parts=$(cli_version_parts "$latest" 2>/dev/null) || return 1

  python3 - "$current_parts" "$latest_parts" <<'PY'
import sys

current = tuple(int(part) for part in sys.argv[1].split())
latest = tuple(int(part) for part in sys.argv[2].split())
sys.exit(0 if current >= latest else 1)
PY
}

install_cli() {
  migrate_legacy_install_if_needed

  if command_exists multica && [ "$(command -v multica)" != "$CLI_BIN_PATH" ]; then
    warn "Detected another multica on PATH at $(command -v multica). The managed install will use $CLI_BIN_PATH."
  fi

  if is_managed_install; then
    local current_ver
    # `multica version` outputs "multica 0.3.23 (commit: ...)" — extract just the version.
    current_ver=$("$CLI_BIN_PATH" version 2>/dev/null | awk '{print $2}' || echo "unknown")

    local latest_ver
    latest_ver=$(get_latest_version)

    if [ -z "$latest_ver" ] || is_cli_version_at_least "$current_ver" "$latest_ver"; then
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
    fail "CLI install failed: binary not present at $CLI_BIN_PATH."
  fi
  if ! command -v multica >/dev/null 2>&1; then
    warn "CLI installed at $CLI_BIN_PATH but not resolvable as 'multica' in this shell."
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

is_daemon_running_for_binary() {
  local binary_path="$1"
  [ -x "$binary_path" ] || return 1
  "$binary_path" daemon status 2>/dev/null | grep -qi "running"
}

stop_daemon_for_binary() {
  local binary_path="$1"
  [ -x "$binary_path" ] || return 0
  if ! is_daemon_running_for_binary "$binary_path"; then
    return 0
  fi

  info "Stopping running Multica daemon before replacing CLI..."
  if ! "$binary_path" daemon stop >/dev/null 2>&1; then
    fail "Failed to stop the running Multica daemon. Please stop it manually and retry."
  fi
}

stop_managed_daemon_for_replace() {
  stop_daemon_for_binary "$CLI_BIN_PATH"
}

uninstall_legacy_install_unix() {
  local candidate candidate_path
  while IFS= read -r candidate; do
    [ -n "$candidate" ] || continue

    case "$candidate" in
      brew)
        info "Detected legacy Homebrew install. Uninstalling $BREW_PACKAGE ..."
        if ! brew uninstall "$BREW_PACKAGE" >/dev/null 2>&1; then
          fail "Failed to uninstall legacy Homebrew package $BREW_PACKAGE."
        fi
        ok "Removed legacy Homebrew install"
        ;;
      path:*)
        candidate_path="${candidate#path:}"
        info "Detected legacy CLI binary at $candidate_path. Removing it before managed install ..."
        stop_daemon_for_binary "$candidate_path"
        if ! rm -f "$candidate_path"; then
          fail "Failed to remove legacy CLI binary at $candidate_path."
        fi
        ok "Removed legacy CLI binary at $candidate_path"
        ;;
    esac
  done <<EOF
$(legacy_install_candidates_unix || true)
EOF
}

migrate_legacy_install_if_needed() {
  if ! has_legacy_install_unix; then
    return 0
  fi

  info "Detected legacy CLI install from the main-branch installer. Migrating to managed manifest install ..."
  uninstall_legacy_install_unix
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
  local server_ref
  server_ref=$(get_selfhost_ref)
  info "Using self-host assets from ${server_ref}..."

  if [ -d "$INSTALL_DIR/.git" ]; then
    info "Updating existing installation at $INSTALL_DIR..."
    cd "$INSTALL_DIR"
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

  checkout_server_ref "$server_ref"

  ok "Repository ready at $INSTALL_DIR ($server_ref)"

  # Generate .env if needed
  if [ ! -f .env ]; then
    info "Creating .env with random secrets..."
    cp .env.example .env
    local jwt pgpass
    jwt=$(openssl rand -hex 32)
    pgpass=$(openssl rand -hex 24)
    if [ "$(uname -s)" = "Darwin" ]; then
      sed -i '' "s/^JWT_SECRET=.*/JWT_SECRET=$jwt/" .env
      sed -i '' "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=$pgpass/" .env
      sed -i '' -E "s#^(DATABASE_URL=postgres://[^:]+:)[^@]*(@.*)#\1$pgpass\2#" .env
    else
      sed -i "s/^JWT_SECRET=.*/JWT_SECRET=$jwt/" .env
      sed -i "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=$pgpass/" .env
      sed -i -E "s#^(DATABASE_URL=postgres://[^:]+:)[^@]*(@.*)#\1$pgpass\2#" .env
    fi
    ok "Generated .env with random JWT_SECRET and POSTGRES_PASSWORD"
  else
    ok "Using existing .env"
  fi

  # Start Docker Compose
  info "Pulling official Multica images..."
  pull_official_selfhost_images
  info "Starting Multica services (this may take a few minutes on first run)..."
  docker compose -f docker-compose.selfhost.yml up -d

  # Wait for health check
  info "Waiting for backend to be ready..."
  local backend_port
  backend_port="$(selfhost_backend_port .env)"
  local ready=false
  for i in $(seq 1 45); do
    if curl -sf "http://localhost:${backend_port}/health" >/dev/null 2>&1; then
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
  local frontend_port backend_port
  frontend_port="$(selfhost_frontend_port "$INSTALL_DIR/.env")"
  backend_port="$(selfhost_backend_port "$INSTALL_DIR/.env")"
  printf "  ${BOLD}Frontend:${RESET}  http://localhost:%s\n" "$frontend_port"
  printf "  ${BOLD}Backend:${RESET}   http://localhost:%s\n" "$backend_port"
  printf "  ${BOLD}Server at:${RESET} %s\n" "$INSTALL_DIR"
  printf "\n"
  printf "  ${BOLD}Next: configure your CLI to connect${RESET}\n"
  printf "\n"
  printf "     ${CYAN}multica setup self-host${RESET}   # Configure + authenticate + start daemon\n"
  printf "\n"
  printf "  ${BOLD}Login:${RESET} configure ${CYAN}RESEND_API_KEY${RESET} in .env for email codes,\n"
  printf "  or read the generated code from backend logs when Resend is unset.\n"
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

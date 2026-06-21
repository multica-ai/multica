#!/usr/bin/env bash
# Multica installer — installs the CLI and optionally provisions a self-host server.
#
# Install / upgrade CLI only:
#   curl -fsSL https://raw.githubusercontent.com/Askhz/multica/main/scripts/install.sh | bash
#
# Install CLI + provision self-host server:
#   curl -fsSL https://raw.githubusercontent.com/Askhz/multica/main/scripts/install.sh | bash -s -- --with-server
#
# After installation, run `cs-workflow setup` to configure your environment.
#
set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
REPO_URL="https://github.com/Askhz/multica.git"
REPO_WEB_URL="https://github.com/Askhz/multica"  # without .git, for GitHub web APIs
INSTALL_DIR="${MULTICA_INSTALL_DIR:-$HOME/.multica/server}"
BREW_PACKAGE="Askhz/tap/multica"

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

detect_os() {
  case "$(uname -s)" in
    Darwin) OS="darwin" ;;
    Linux)  OS="linux" ;;
    MINGW*|MSYS*|CYGWIN*)
            fail "This script does not support Windows. Use the PowerShell installer instead:
  irm https://raw.githubusercontent.com/Askhz/multica/main/scripts/install.ps1 | iex" ;;
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
_dump_brew_log() {
  local log="$1"
  if [ -s "$log" ]; then
    warn "Homebrew output (last 80 lines):"
    tail -n 80 "$log" | sed 's/^/  /' >&2
  fi
}

install_cli_brew() {
  info "Installing Multica CLI via Homebrew..."
  local brew_log
  brew_log=$(mktemp)
  if ! brew tap Askhz/tap >"$brew_log" 2>&1; then
    warn "Failed to add Homebrew tap. Falling back to GitHub Releases binary install."
    _dump_brew_log "$brew_log"
    rm -f "$brew_log"
    return 1
  fi
  # brew install exits non-zero if already installed on older Homebrew versions
  if ! brew install "$BREW_PACKAGE" >"$brew_log" 2>&1; then
    if brew list "$BREW_PACKAGE" >/dev/null 2>&1; then
      rm -f "$brew_log"
      ok "Multica CLI already installed via Homebrew"
    else
      warn "Failed to install multica via Homebrew. Falling back to GitHub Releases binary install."
      _dump_brew_log "$brew_log"
      rm -f "$brew_log"
      return 1
    fi
  else
    rm -f "$brew_log"
    ok "Multica CLI installed via Homebrew"
  fi
}

install_cli_binary() {
  info "Installing Multica CLI from GitHub Releases..."

  # Get latest release tag
  local latest
  latest=$(curl -sI "$REPO_WEB_URL/releases/latest" 2>/dev/null | grep -i '^location:' | sed 's/.*tag\///' | tr -d '\r\n' || true)
  if [ -z "$latest" ]; then
    fail "Could not determine latest release. Check your network connection."
  fi

  local version="${latest#v}"
  local url="https://github.com/Askhz/multica/releases/download/${latest}/cs-workflow-cli-${version}-${OS}-${ARCH}.tar.gz"
  local tmp_dir
  tmp_dir=$(mktemp -d)

  info "Downloading $url ..."
  if ! curl -fsSL "$url" -o "$tmp_dir/cs-workflow.tar.gz"; then
    rm -rf "$tmp_dir"
    fail "Failed to download CLI binary."
  fi

  tar -xzf "$tmp_dir/cs-workflow.tar.gz" -C "$tmp_dir" cs-workflow

  # Try /usr/local/bin first, fall back to ~/.local/bin. Tests and scripted
  # installs can override the first choice with MULTICA_BIN_DIR.
  local bin_dir="${MULTICA_BIN_DIR:-/usr/local/bin}"
  if [ -w "$bin_dir" ]; then
    mv "$tmp_dir/cs-workflow" "$bin_dir/cs-workflow"
  elif command_exists sudo; then
    sudo mv "$tmp_dir/cs-workflow" "$bin_dir/cs-workflow"
  else
    bin_dir="$HOME/.local/bin"
    mkdir -p "$bin_dir"
    mv "$tmp_dir/cs-workflow" "$bin_dir/cs-workflow"
    chmod +x "$bin_dir/cs-workflow"
    # Add to PATH if not already there
    if ! echo "$PATH" | tr ':' '\n' | grep -q "^$bin_dir$"; then
      export PATH="$bin_dir:$PATH"
      add_to_path "$bin_dir"
    fi
  fi

  rm -rf "$tmp_dir"
  ok "Multica CLI installed to $bin_dir/cs-workflow"
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
  # GitHub API is more reliable than parsing redirect headers from `curl -sI`.
  # The unauthenticated API has a 60 req/hour rate limit — fine for a manual install.
  local tag
  tag=$(curl -sfL "https://api.github.com/repos/Askhz/multica/releases/latest" 2>/dev/null | grep -o '"tag_name": "[^"]*"' | head -n 1 | sed 's/.*"tag_name": "\([^"]*\)".*/\1/')
  if [ -n "$tag" ]; then
    printf '%s' "$tag"
    return
  fi

  # Fallback: try parsing the redirect Location header.
  tag=$(curl -sI "$REPO_WEB_URL/releases/latest" 2>/dev/null | grep -i '^location:' | sed 's/.*tag\///' | tr -d '\r\n')
  if [ -n "$tag" ]; then
    printf '%s' "$tag"
    return
  fi

  # Nothing worked — caller handles empty as "cannot determine".
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

install_cli() {
  if command_exists cs-workflow; then
    local current_ver
    # `cs-workflow version` outputs "cs-workflow v0.1.13 (commit: abc1234)" — extract just the version
    current_ver=$(cs-workflow version 2>/dev/null | awk 'NR==1{print $2}' || echo "unknown")

    # Interactive prompt when a terminal is available.
    if [ -t 0 ]; then
      printf "${BOLD}${YELLOW}⚠ Multica CLI (%s) is already installed. Overwrite? [y/N] ${RESET}" "$current_ver"
      read -r answer
      case "$answer" in
        [yY]|[yY][eE][sS])
          info "Removing existing cs-workflow..."
          rm -f "$(command -v cs-workflow)"
          ;;
        *)
          info "Installation cancelled."
          exit 0
          ;;
      esac
    else
      # Non-interactive (piped/CI): keep existing behaviour — attempt upgrade.
      local latest_ver
      latest_ver=$(get_latest_version)

      local current_cmp="${current_ver#v}"
      local latest_cmp="${latest_ver#v}"

      if [ -z "$latest_ver" ]; then
        warn "Could not check latest release from GitHub. Will attempt to re-install anyway..."
        install_cli_binary
        return 0
      fi

      if [ "$current_cmp" = "$latest_cmp" ]; then
        ok "Multica CLI is up to date ($current_ver)"
        return 0
      fi

      info "Multica CLI $current_ver installed, latest is $latest_ver — upgrading..."
      install_cli_binary

      local new_ver
      new_ver=$(cs-workflow version 2>/dev/null | awk 'NR==1{print $2}' || echo "unknown")
      ok "Multica CLI upgraded ($current_ver → $new_ver)"
      return 0
    fi
  fi

  install_cli_binary

  # Verify
  if ! command_exists cs-workflow; then
    fail "CLI installed but 'cs-workflow' not found on PATH. You may need to restart your shell."
  fi
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
  info "Pulling official Multica images..."
  pull_official_selfhost_images
  info "Starting Multica services (this may take a few minutes on first run)..."
  docker compose -f docker-compose.selfhost.yml up -d

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
  printf "\n"

  detect_os
  install_cli

  printf "\n"
  printf "${BOLD}${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}\n"
  printf "${BOLD}${GREEN}  ✓ Multica CLI is ready!${RESET}\n"
  printf "${BOLD}${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}\n"
  printf "\n"
  printf "  ${BOLD}Next: configure your environment${RESET}\n"
  printf "\n"
  printf "     ${CYAN}cs-workflow config set server_url <YOUR_SERVER_URL>${RESET}\n"
  printf "     ${CYAN}cs-workflow login --token <YOUR_TOKEN>${RESET}\n"
  printf "     ${CYAN}cs-workflow daemon start${RESET}\n"
  printf "\n"
  printf "  ${BOLD}Get your token:${RESET} open the web dashboard → Settings → Tokens\n"
  printf "  ${BOLD}Self-hosting?${RESET} Install the server first:\n"
  printf "     curl -fsSL https://raw.githubusercontent.com/Askhz/multica/main/scripts/install.sh | bash -s -- --with-server\n"
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
  printf "     ${CYAN}cs-workflow setup self-host${RESET}   # Configure + authenticate + start daemon\n"
  printf "\n"
  printf "  ${BOLD}Login:${RESET} configure ${CYAN}RESEND_API_KEY${RESET} in .env for email codes,\n"
  printf "  or read the generated code from backend logs when Resend is unset.\n"
  printf "\n"
  printf "  ${BOLD}To stop all services:${RESET}\n"
  printf "     curl -fsSL https://raw.githubusercontent.com/Askhz/multica/main/scripts/install.sh | bash -s -- --stop\n"
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

  if command_exists cs-workflow; then
    cs-workflow daemon stop 2>/dev/null && ok "Daemon stopped" || true
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
        echo "  (default)       Install / upgrade the Multica CLI"
        echo "  --with-server   Install CLI + provision a self-host server (Docker)"
        echo "  --stop          Stop a self-hosted installation"
        echo ""
        echo "Environment variables:"
        echo "  MULTICA_INSTALL_DIR   Self-host server install directory"
        echo "                        (default: \$HOME/.multica/server)"
        echo "  MULTICA_BIN_DIR       Target directory for the CLI binary when"
        echo "                        installing from GitHub Releases"
        echo "                        (default: /usr/local/bin, then \$HOME/.local/bin)"
        echo "  MULTICA_SELFHOST_REF  Git ref to check out for self-host assets"
        echo "                        (default: latest release tag, falling back to main)"
        echo ""
        echo "After installation, run 'cs-workflow setup' to configure your environment."
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

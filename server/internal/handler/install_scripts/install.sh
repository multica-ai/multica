#!/bin/sh
# Multica CLI installer — https://multica.wujieai.com
# Usage:
#   curl -fsSL https://multica.wujieai.com/install.sh | sh
#   curl -fsSL https://multica.wujieai.com/install.sh | sh -s -- --version 0.3.1-514-gc59dc875
#   curl -fsSL https://multica.wujieai.com/install.sh | sh -s -- --channel test
#   MULTICA_VERSION=0.3.1-514-gc59dc875 sh install.sh
#
# Environment variables:
#   MULTICA_VERSION   — install a specific version instead of latest
#   MULTICA_DIR       — installation directory (default: ~/.multica/bin)
#   MULTICA_SERVER    — server URL (default: https://multica.wujieai.com)
#   MULTICA_CHANNEL   — release channel: prod (default) or test
set -e

# --- Configuration ---
DEFAULT_SERVER="https://multica.wujieai.com"
OBS_HOST="https://multica.obs.cn-east-3.myhuaweicloud.com"
CHANNEL="${MULTICA_CHANNEL:-}"
INSTALL_DIR="${MULTICA_DIR:-$HOME/.multica/bin}"
SERVER_URL="${MULTICA_SERVER:-$DEFAULT_SERVER}"

# --- Colors ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

info()  { printf "${BLUE}[info]${NC}  %s\n" "$1"; }
ok()    { printf "${GREEN}[ok]${NC}    %s\n" "$1"; }
warn()  { printf "${YELLOW}[warn]${NC}  %s\n" "$1"; }
err()   { printf "${RED}[error]${NC} %s\n" "$1" >&2; }
die()   { err "$1"; exit 1; }

# --- Parse arguments ---
VERSION="${MULTICA_VERSION:-}"
while [ $# -gt 0 ]; do
    case "$1" in
        --version|-v)
            VERSION="$2"
            shift 2
            ;;
        --dir)
            INSTALL_DIR="$2"
            shift 2
            ;;
        --server)
            SERVER_URL="$2"
            shift 2
            ;;
        --channel)
            CHANNEL="$2"
            shift 2
            ;;
        --help|-h)
            echo "Usage: install.sh [--version VERSION] [--dir DIR] [--server URL] [--channel CHANNEL]"
            echo ""
            echo "Options:"
            echo "  --version VERSION   Install a specific CLI version"
            echo "  --dir DIR           Installation directory (default: ~/.multica/bin)"
            echo "  --server URL        Multica server URL (default: $DEFAULT_SERVER)"
            echo "  --channel CHANNEL   Release channel: prod (default) or test"
            exit 0
            ;;
        *)
            warn "Unknown argument: $1"
            shift
            ;;
    esac
done

# --- Resolve OBS paths based on channel ---
if [ "$CHANNEL" = "test" ]; then
    OBS_PREFIX="cli-test"
else
    OBS_PREFIX="cli"
fi
OBS_BASE="${OBS_HOST}/${OBS_PREFIX}/releases"
MANIFEST_URL="${OBS_HOST}/${OBS_PREFIX}/manifest.json"

# --- Detect OS and architecture ---
detect_platform() {
    OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
    ARCH="$(uname -m)"

    case "$OS" in
        linux)   OS="linux" ;;
        darwin)  OS="darwin" ;;
        mingw*|msys*|cygwin*)
            OS="windows"
            ;;
        *)
            die "Unsupported operating system: $OS"
            ;;
    esac

    case "$ARCH" in
        x86_64|amd64)   ARCH="amd64" ;;
        aarch64|arm64)  ARCH="arm64" ;;
        *)
            die "Unsupported architecture: $ARCH"
            ;;
    esac
}

# --- Check for required tools ---
check_deps() {
    for cmd in curl tar; do
        if ! command -v "$cmd" >/dev/null 2>&1; then
            die "Required command not found: $cmd"
        fi
    done
}

# --- Fetch latest version ---
fetch_latest_version() {
    info "Fetching latest CLI version..."

    # Try the server endpoint first (returns plain text, no JSON parsing needed)
    VERSION=$(curl -fsSL "${SERVER_URL}/install/latest-cli-version" 2>/dev/null | tr -d '[:space:]') || true

    if [ -z "$VERSION" ]; then
        # Fallback: parse manifest.json from OBS
        info "Falling back to OBS manifest..."
        if command -v python3 >/dev/null 2>&1; then
            VERSION=$(curl -fsSL "$MANIFEST_URL" 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin).get('version','').lstrip('v'))" 2>/dev/null) || true
        elif command -v python >/dev/null 2>&1; then
            VERSION=$(curl -fsSL "$MANIFEST_URL" 2>/dev/null | python -c "import sys,json; print(json.load(sys.stdin).get('version','').lstrip('v'))" 2>/dev/null) || true
        fi
    fi

    if [ -z "$VERSION" ]; then
        die "Failed to determine latest CLI version. Try specifying --version manually."
    fi

    # Strip leading 'v' if present
    VERSION="${VERSION#v}"
}

# --- Fetch checksum from manifest ---
fetch_checksum() {
    local filename="$1"
    EXPECTED_CHECKSUM=""

    if command -v python3 >/dev/null 2>&1; then
        EXPECTED_CHECKSUM=$(curl -fsSL "$MANIFEST_URL" 2>/dev/null | python3 -c "
import sys, json
m = json.load(sys.stdin)
for a in m.get('assets', []):
    if a.get('filename') == '$filename':
        print(a.get('checksum', ''))
        break
" 2>/dev/null) || true
    elif command -v python >/dev/null 2>&1; then
        EXPECTED_CHECKSUM=$(curl -fsSL "$MANIFEST_URL" 2>/dev/null | python -c "
import sys, json
m = json.load(sys.stdin)
for a in m.get('assets', []):
    if a.get('filename') == '$filename':
        print(a.get('checksum', ''))
        break
" 2>/dev/null) || true
    fi
}

# --- Verify checksum ---
verify_checksum() {
    local file="$1"
    local expected="$2"

    if [ -z "$expected" ]; then
        warn "No checksum available, skipping verification"
        return 0
    fi

    local actual=""
    if command -v sha256sum >/dev/null 2>&1; then
        actual=$(sha256sum "$file" | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then
        actual=$(shasum -a 256 "$file" | awk '{print $1}')
    else
        warn "No sha256sum or shasum found, skipping checksum verification"
        return 0
    fi

    if [ "$actual" != "$expected" ]; then
        die "Checksum verification failed!\n  Expected: $expected\n  Actual:   $actual"
    fi
    ok "Checksum verified"
}

# --- Download and install ---
download_and_install() {
    local ext="tar.gz"
    if [ "$OS" = "windows" ]; then
        ext="zip"
    fi

    local filename="multica-cli-${VERSION}-${OS}-${ARCH}.${ext}"
    local url="${OBS_BASE}/${filename}"

    info "Downloading Multica CLI v${VERSION} for ${OS}/${ARCH}..."
    info "URL: ${url}"

    # Create temp directory
    local tmpdir
    tmpdir=$(mktemp -d)
    trap "rm -rf '$tmpdir'" EXIT

    # Download
    if ! curl -fSL --progress-bar -o "${tmpdir}/${filename}" "$url"; then
        die "Download failed. The version '${VERSION}' may not exist for ${OS}/${ARCH}.\nURL: ${url}"
    fi

    # Check file is not empty
    if [ ! -s "${tmpdir}/${filename}" ]; then
        die "Downloaded file is empty"
    fi

    # Verify checksum
    fetch_checksum "$filename"
    verify_checksum "${tmpdir}/${filename}" "$EXPECTED_CHECKSUM"

    # Extract
    info "Installing to ${INSTALL_DIR}..."
    mkdir -p "$INSTALL_DIR"

    if [ "$ext" = "tar.gz" ]; then
        tar -xzf "${tmpdir}/${filename}" -C "$tmpdir"
    else
        # Windows zip
        if command -v unzip >/dev/null 2>&1; then
            unzip -q "${tmpdir}/${filename}" -d "$tmpdir"
        else
            die "unzip command not found, cannot extract .zip archive"
        fi
    fi

    # Find the binary
    local binary_name="multica"
    if [ "$OS" = "windows" ]; then
        binary_name="multica.exe"
    fi

    local binary_src
    binary_src=$(find "$tmpdir" -name "$binary_name" -type f | head -1)
    if [ -z "$binary_src" ]; then
        die "Binary '$binary_name' not found in archive"
    fi

    # Install
    cp "$binary_src" "${INSTALL_DIR}/${binary_name}"
    chmod +x "${INSTALL_DIR}/${binary_name}"

    # macOS: ad-hoc sign to prevent Gatekeeper from killing the binary
    if [ "$OS" = "darwin" ]; then
        codesign --force --sign - "${INSTALL_DIR}/${binary_name}" 2>/dev/null || true
    fi

    ok "Installed ${INSTALL_DIR}/${binary_name}"
}

# --- Configure PATH ---
configure_path() {
    local shell_profile=""
    local path_entry="export PATH=\"${INSTALL_DIR}:\$PATH\""

    # Check if already in PATH
    case ":$PATH:" in
        *":${INSTALL_DIR}:"*)
            return 0
            ;;
    esac

    # Detect shell profile
    if [ -n "${ZSH_VERSION:-}" ] || [ "$(basename "$SHELL" 2>/dev/null)" = "zsh" ]; then
        shell_profile="$HOME/.zshrc"
    elif [ -n "${BASH_VERSION:-}" ] || [ "$(basename "$SHELL" 2>/dev/null)" = "bash" ]; then
        if [ -f "$HOME/.bashrc" ]; then
            shell_profile="$HOME/.bashrc"
        elif [ -f "$HOME/.bash_profile" ]; then
            shell_profile="$HOME/.bash_profile"
        fi
    fi

    if [ -n "$shell_profile" ] && [ -f "$shell_profile" ]; then
        if ! grep -q "\.multica/bin" "$shell_profile" 2>/dev/null; then
            printf '\n# Multica CLI\n%s\n' "$path_entry" >> "$shell_profile"
            info "Added ${INSTALL_DIR} to PATH in ${shell_profile}"
        fi
    fi

    # Also export for current session
    export PATH="${INSTALL_DIR}:$PATH"

    if ! command -v multica >/dev/null 2>&1; then
        warn "${INSTALL_DIR} is not in your PATH."
        warn "Add the following to your shell profile:"
        echo ""
        echo "  $path_entry"
        echo ""
    fi
}

# --- Configure server URL ---
configure_server() {
    local multica_bin="${INSTALL_DIR}/multica"

    info "Configuring server URL: ${SERVER_URL}"
    "$multica_bin" config set server_url "$SERVER_URL" 2>/dev/null || true
    "$multica_bin" config set app_url "$SERVER_URL" 2>/dev/null || true
    ok "Server configured: ${SERVER_URL}"
}

# --- Restart daemon ---
restart_daemon() {
    local multica_bin="${INSTALL_DIR}/multica"

    info "Restarting daemon..."
    "$multica_bin" daemon stop 2>/dev/null || true
    sleep 1
    if "$multica_bin" daemon start 2>/dev/null; then
        ok "Daemon started"
    else
        warn "Failed to start daemon. You can start it manually: multica daemon start"
    fi
}

# --- Print summary ---
print_summary() {
    local multica_bin="${INSTALL_DIR}/multica"
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    ok "Multica CLI installed successfully!"
    echo ""
    printf "  Version:  %s\n" "$("$multica_bin" version 2>/dev/null || echo "v${VERSION}")"
    printf "  Binary:   %s\n" "${INSTALL_DIR}/multica"
    printf "  Server:   %s\n" "$SERVER_URL"
    echo ""
    echo "  Next step: Log in to your Multica account:"
    echo ""
    echo "    multica login"
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

# --- Main ---
main() {
    echo ""
    info "Multica CLI Installer"
    echo ""

    check_deps
    detect_platform

    if [ -z "$VERSION" ]; then
        fetch_latest_version
    else
        VERSION="${VERSION#v}"
        info "Installing specified version: ${VERSION}"
    fi

    download_and_install
    configure_path
    configure_server
    if [ "$CHANNEL" = "test" ]; then
        info "Configuring update manifest for test channel..."
        "${INSTALL_DIR}/multica" config set update_manifest_url "$MANIFEST_URL" 2>/dev/null || true
        ok "Update manifest set to test channel: ${MANIFEST_URL}"
    fi
    restart_daemon
    print_summary
}

main

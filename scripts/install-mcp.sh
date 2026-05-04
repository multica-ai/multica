#!/usr/bin/env bash
# Multica MCP installer — installs the CLI (if missing), authenticates,
# and registers the MCP server with Claude Code.
#
# One-line install (requires gh CLI with access to firtal-group):
#   gh repo view firtal-group/firtal-cerebro --raw --ref main scripts/install-mcp.sh | bash
#
set -euo pipefail

REPO="firtal-group/firtal-cerebro"

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

info()  { printf "${BOLD}${CYAN}==> %s${RESET}\n" "$*"; }
ok()    { printf "${BOLD}${GREEN}✓ %s${RESET}\n" "$*"; }
warn()  { printf "${BOLD}${YELLOW}⚠ %s${RESET}\n" "$*" >&2; }
fail()  { printf "${BOLD}${RED}✗ %s${RESET}\n" "$*" >&2; exit 1; }

command_exists() { command -v "$1" >/dev/null 2>&1; }

# Fetch a file from the private repo using gh CLI (handles auth automatically)
fetch_repo_file() {
  local path="$1"
  gh api "repos/${REPO}/contents/${path}?ref=main" --jq '.content' | base64 -d
}

ensure_gh_cli() {
  if command_exists gh; then
    return 0
  fi
  fail "'gh' CLI not found on PATH.

The installer uses the GitHub CLI to fetch files from the private firtal-group repo.
Install GitHub CLI first: https://cli.github.com/

Then authenticate with: gh auth login
And re-run this installer."
}

ensure_gh_auth() {
  if gh auth status >/dev/null 2>&1; then
    return 0
  fi
  fail "GitHub CLI is not authenticated.

Run: gh auth login

Then re-run this installer."
}

ensure_claude_cli() {
  if command_exists claude; then
    return 0
  fi
  fail "'claude' CLI not found on PATH.

The Multica MCP registers itself with Claude Code via the 'claude' CLI.
Install Claude Code first: https://claude.com/claude-code

Then re-run this installer."
}

ensure_multica_cli() {
  if command_exists multica; then
    ok "Multica CLI already installed ($(multica version 2>/dev/null | awk '{print $2}' || echo "unknown"))"
    return 0
  fi

  info "Installing Multica CLI..."
  # Fetch and run the canonical CLI installer from the private repo
  fetch_repo_file "scripts/install.sh" | bash

  if ! command_exists multica; then
    fail "Multica CLI installed but 'multica' not found on PATH. Restart your shell and re-run this installer."
  fi
}

ensure_logged_in() {
  if multica auth status >/dev/null 2>&1; then
    ok "Already authenticated"
    return 0
  fi

  info "Authenticating with Multica..."
  printf "${BOLD}A browser window will open. Complete sign-in, then return here.${RESET}\n"
  if ! multica login; then
    fail "Login failed. Run 'multica login' manually, then re-run this installer."
  fi
}

register_mcp() {
  info "Registering Multica MCP with Claude Code..."
  if ! multica mcp install; then
    fail "MCP registration failed. See the message above for details."
  fi
}

main() {
  printf "\n"
  printf "${BOLD}  Multica MCP — Installer${RESET}\n"
  printf "\n"

  ensure_gh_cli
  ensure_gh_auth
  ensure_claude_cli
  ensure_multica_cli
  ensure_logged_in
  register_mcp

  printf "\n"
  printf "${BOLD}${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}\n"
  printf "${BOLD}${GREEN}  ✓ Multica MCP is ready in Claude Code${RESET}\n"
  printf "${BOLD}${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}\n"
  printf "\n"
  printf "  Restart Claude Code if it's already running, then run ${CYAN}/mcp${RESET} to verify.\n"
  printf "\n"
}

main "$@"

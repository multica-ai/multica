#!/bin/bash
set -euo pipefail

# ── Mandatory env ─────────────────────────────────────────────────────────────
: "${MULTICA_PAT:?MULTICA_PAT required}"
: "${MULTICA_WORKSPACE_ID:?MULTICA_WORKSPACE_ID required}"
: "${ANTHROPIC_API_KEY:?ANTHROPIC_API_KEY required}"
: "${OPENAI_API_KEY:?OPENAI_API_KEY required}"
: "${WORKSPACE_SLUG:?WORKSPACE_SLUG required}"

# ── GitHub credential helper (Enterprise Platform Bot GitHub App) ─────────────
# The runner mints short-lived installation tokens via git-credential-platform-bot.
# Installation IDs are resolved dynamically per org/repo at mint time, so no
# GITHUB_APP_INSTALLATION_ID is needed. App creds (GITHUB_APP_ID,
# GITHUB_APP_PRIVATE_KEY) arrive via the deployment env + agentrunner-secrets.
if [ -n "${GITHUB_APP_ID:-}" ] && [ -n "${GITHUB_APP_PRIVATE_KEY:-}" ]; then
  git config --global --replace-all credential."https://github.com".helper "/usr/local/bin/git-credential-platform-bot"
  # Send the full repo path to the credential helper so it can resolve the
  # correct installation per org without a hardcoded installation ID.
  git config --global credential."https://github.com".useHttpPath true
  # Rewrite ssh-style remotes to https so the credential helper applies.
  git config --global --unset-all url."https://github.com/".insteadOf 2>/dev/null || true
  git config --global --add url."https://github.com/".insteadOf "git@github.com:"
  git config --global --add url."https://github.com/".insteadOf "ssh://git@github.com/"
  # gh doesn't use git's credential helper; the /usr/local/bin/gh wrapper injects
  # a fresh token per invocation (same on-demand minting as git), so there's no
  # long-lived gh token to keep alive over the pod's multi-month life.
fi

# ── Git identity ──────────────────────────────────────────────────────────────
# Commits from runner agents are bot-authored. Defaults target the Platform Bot
# and are overridable via the deployment env for precise attribution.
git config --global user.name  "${GIT_USER_NAME:-g2-platform-bot[bot]}"
git config --global user.email "${GIT_USER_EMAIL:-g2-platform-bot[bot]@users.noreply.github.com}"

# ── SSH key ───────────────────────────────────────────────────────────────────
SSH_DIR="${HOME}/.ssh"
SSH_KEY="${SSH_DIR}/id_ed25519"
mkdir -p "${SSH_DIR}"
chmod 700 "${SSH_DIR}" 2>/dev/null || true
if [ ! -f "${SSH_KEY}" ]; then
  ssh-keygen -t ed25519 -N "" -f "${SSH_KEY}" -C "${GIT_USER_EMAIL:-agent@agentrunner}" >/dev/null
fi
chmod 600 "${SSH_KEY}" 2>/dev/null || true
chmod 644 "${SSH_KEY}.pub" 2>/dev/null || true

# ── Write multica config ───────────────────────────────────────────────────────
# Defaults to the tools/prod server; override via env (e.g. the dev runner
# pipeline points this at the development agentfarm server).
readonly MULTICA_SERVER_URL="${MULTICA_SERVER_URL:-https://agentfarm.g2.com}"
config_dir="${HOME}/.multica"
mkdir -p "${config_dir}"
umask 077
cat > "${config_dir}/config.json" <<JSON
{
  "server_url": "${MULTICA_SERVER_URL}",
  "app_url": "${MULTICA_SERVER_URL}",
  "token": "${MULTICA_PAT}",
  "workspace_id": "${MULTICA_WORKSPACE_ID}"
}
JSON

DEVICE_NAME="agentrunner-${WORKSPACE_SLUG}"

# ── Provision agents once daemon registers (waits in background) ──────────────
/usr/local/bin/agentfarm-bootstrap.sh &

# ── Run daemon in foreground ──────────────────────────────────────────────────
exec multica daemon start --foreground --device-name "${DEVICE_NAME}"

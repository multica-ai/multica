#!/bin/bash
set -euo pipefail

# ── Mandatory env ─────────────────────────────────────────────────────────────
: "${MULTICA_PAT:?MULTICA_PAT required}"
: "${AGENTFARM_WORKSPACE_ID:?AGENTFARM_WORKSPACE_ID required}"
: "${ANTHROPIC_API_KEY:?ANTHROPIC_API_KEY required}"
: "${OPENAI_API_KEY:?OPENAI_API_KEY required}"
: "${WORKSPACE_SLUG:?WORKSPACE_SLUG required (Downward API: metadata.namespace)}"

# ── GitHub credential helper ──────────────────────────────────────────────────
# gandalf writes the PAT as GITHUB_PAT; gh reads GH_TOKEN, so bridge the two.
export GH_TOKEN="${GH_TOKEN:-${GITHUB_PAT:-}}"
if [ -n "${GH_TOKEN:-}" ]; then
  if gh auth setup-git --hostname github.com; then
    git config --global --unset-all url."https://github.com/".insteadOf 2>/dev/null || true
    git config --global --add url."https://github.com/".insteadOf "git@github.com:"
    git config --global --add url."https://github.com/".insteadOf "ssh://git@github.com/"
  fi
fi

# ── Git identity ──────────────────────────────────────────────────────────────
if [ -n "${GIT_USER_NAME:-}" ];  then git config --global user.name  "$GIT_USER_NAME";  fi
if [ -n "${GIT_USER_EMAIL:-}" ]; then git config --global user.email "$GIT_USER_EMAIL"; fi

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
readonly MULTICA_SERVER_URL="https://agentfarm.g2.com"
config_dir="${HOME}/.multica"
mkdir -p "${config_dir}"
umask 077
cat > "${config_dir}/config.json" <<JSON
{
  "server_url": "${MULTICA_SERVER_URL}",
  "app_url": "${MULTICA_SERVER_URL}",
  "token": "${MULTICA_PAT}",
  "workspace_id": "${AGENTFARM_WORKSPACE_ID}"
}
JSON

# Trim agentrunner- prefix: namespace is "agentrunner-<slug>", device name must match.
SLUG="${WORKSPACE_SLUG#agentrunner-}"
DEVICE_NAME="agentrunner-${SLUG}"

# ── Provision agents once daemon registers (waits in background) ──────────────
/usr/local/bin/agentfarm-bootstrap.sh &

# ── Run daemon in foreground ──────────────────────────────────────────────────
exec multica daemon start --foreground --device-name "${DEVICE_NAME}"

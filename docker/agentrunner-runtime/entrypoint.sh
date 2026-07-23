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

# ── Optional one-off tool installs ─────────────────────────────────────────────
# EXTRA_UV_TOOLS lets a single workspace opt into extra CLIs (e.g. `snow`, the
# Snowflake CLI) without baking them into agent-runtime-base for every
# workspace. Space-separated list of `uv tool install` targets, always
# version-pinned (e.g. "snowflake-cli==3.23.0" — uv uses PEP 508 `==`, a bare
# `=` is rejected) since this path is best-effort and untested by CI, so an
# unpinned entry can silently resolve to a different release on the next pod
# boot. Land it as an SSM param under this workspace's slug
# (/agentfarm/development/agentrunner/<slug>/EXTRA_UV_TOOLS) and it arrives
# here as a normal env var via the existing ExternalSecret sweep — no image
# change, no per-workspace Dockerfile. Best-effort: a failed install logs a
# warning and does not block pod boot, since this is a convenience, not a
# dependency anything else here relies on. See ROIPPC-2 for the fuller
# discussion of one-off/custom tooling needs.
if [ -n "${EXTRA_UV_TOOLS:-}" ]; then
  echo "entrypoint: installing extra uv tools: ${EXTRA_UV_TOOLS}"
  for tool in ${EXTRA_UV_TOOLS}; do
    uv tool install "${tool}" || echo "entrypoint: WARNING failed to install extra uv tool '${tool}' (continuing)" >&2
  done
fi

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

# ── Seed ~/.agents/skills from ai-enhancement-hub ────────────────────────────
echo "agentrunner: seeding ai-enhancement-hub skills..."
if [ ! -d "${HOME}/ai-enhancement-hub" ]; then
  git clone --depth=1 https://github.com/g2crowd/ai-enhancement-hub "${HOME}/ai-enhancement-hub"
else
  git -C "${HOME}/ai-enhancement-hub" pull --ff-only
fi
mkdir -p "${HOME}/.agents/skills"
cp -r "${HOME}/ai-enhancement-hub/skills/." "${HOME}/.agents/skills/"
rm -rf "${HOME}/ai-enhancement-hub"

# ── Run daemon in foreground ──────────────────────────────────────────────────
exec multica daemon start --foreground --device-name "${DEVICE_NAME}"

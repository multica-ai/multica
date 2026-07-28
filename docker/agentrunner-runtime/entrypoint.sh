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

# ── Warm hermes's update-check cache ──────────────────────────────────────────
# `hermes --version` is not a pure version print: cmd_version() in hermes_cli
# also runs check_for_updates(), which shells out to `git fetch origin`
# against github.com/NousResearch/hermes-agent (up to a 10s timeout) unless a
# cache file under ~/.hermes is less than 6h old. The daemon calls
# `hermes --version` once per workspace during boot-time runtime registration
# (registerRuntimesForWorkspace), each with only a 10s budget. If the *first*
# such call lands during slow GitHub egress and gets killed before the cache
# is written, every later workspace's call retries the same slow fetch and can
# also miss the timeout — this is what was causing hermes registrations to
# fail intermittently across a workspace fleet (AIPLAT-154). Priming the cache
# here, before the daemon starts, means all of the daemon's later per-workspace
# probes hit a warm cache and return in well under a second. Bounded and
# best-effort: a slow or failed warm-up (offline, GitHub down) just means the
# daemon's own per-call timeout is the fallback, same as before this existed.
timeout 15 hermes --version >/dev/null 2>&1 || true

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
# `uv tool install` links executables into ~/.local/bin. Login shells pick this
# up via ~/.profile, but the daemon exec'd at the bottom of this script (and
# every agent tool-call subprocess it spawns) is not a login shell, so without
# this export a bare `snow` (or any other EXTRA_UV_TOOLS binary) is
# "command not found" even though the install above succeeded. Exporting here,
# before the final exec, makes it inherited by the whole process tree.
export PATH="${HOME}/.local/bin:${PATH}"

# ── Materialize PEM secrets as files ───────────────────────────────────────────
# gitops/base/agent-runtime/external-secret.yaml sweeps every SSM param under
# the workspace's slug (and /shared/*) into this container as a plain env var —
# so a PEM secret always arrives as an env var, never a file. That's fine for
# tools that accept a key on stdin/fd (see git-credential-platform-bot.sh's
# `<(printf '%s' "$GITHUB_APP_PRIVATE_KEY")` trick), but tools like `snow
# --private-key-file` only accept a real path on disk. Bridge the gap once
# per pod boot: any `<NAME>_PRIVATE_KEY` env var whose value looks like PEM
# gets written to a pod-lifetime file and gets a sibling `<NAME>_PRIVATE_KEY_FILE`
# exported pointing at it, so downstream tooling never has to special-case
# "where did this secret come from" (ROIPPC-2). `${SECRETS_DIR}` is backed by a
# tmpfs (`emptyDir: {medium: Memory}`) volume mounted in
# gitops/base/agent-runtime/deployment.yaml, so the material lives in RAM for
# the pod's lifetime and is never written to the node's disk.
SECRETS_DIR="${HOME}/.secrets"
mkdir -p "${SECRETS_DIR}"
# ${SECRETS_DIR} is the mount point of the emptyDir{medium: Memory} volume in
# gitops/base/agent-runtime/deployment.yaml, created root-owned with group
# 1000 via fsGroup. fsGroup grants the non-root agent user rw access but not
# chmod (that needs ownership or CAP_FOWNER), so this fails with EPERM —
# harmless, since fsGroup already restricts the dir to owner/group only.
chmod 700 "${SECRETS_DIR}" 2>/dev/null || true
while IFS='=' read -r -d '' env_name env_value; do
  case "${env_name}" in
    *_PRIVATE_KEY)
      case "${env_value}" in
        -----BEGIN*)
          key_file="${SECRETS_DIR}/${env_name}"
          ( umask 077 && printf '%s\n' "${env_value}" > "${key_file}" )
          chmod 600 "${key_file}"
          export "${env_name}_FILE=${key_file}"
          echo "entrypoint: materialized \${${env_name}} -> \${${env_name}_FILE}=${key_file}"
          ;;
      esac
      ;;
  esac
done < <(env -0)

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

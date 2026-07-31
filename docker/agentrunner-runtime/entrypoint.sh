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

# ── Hermes provider default ───────────────────────────────────────────────────
# Hermes' provider auto-detection (hermes_cli.auth.resolve_provider) treats a
# set OPENAI_API_KEY as "use openrouter", not "use the native OpenAI
# provider" — and OPENAI_API_KEY is exported unconditionally above for the
# claude/codex runtimes. Without an explicit model.provider, Hermes always
# mis-resolves to openrouter, finds no OPENROUTER_API_KEY, and every call
# fails with "HTTP 401: Missing Authentication header" (upstream issue
# #42130). Seed config.yaml with the OpenAI-native provider — which does
# read OPENAI_API_KEY — before Hermes gets a chance to self-create an empty
# one.
#
# We also pin model.api_mode to codex_responses. All of our OpenAI-shaped
# traffic (including hermes) is routed through our internal LiteLLM gateway
# via OPENAI_BASE_URL, not api.openai.com directly. Hermes only force-selects
# the Responses API wire protocol (hermes_cli.providers.host_mandated_api_mode)
# for a literal api.openai.com hostname; any other host — including our
# gateway — defaults to the chat_completions wire protocol. Reasoning models
# (gpt-5.6-terra, gpt-5.5, ...) reject chat_completions requests that combine
# function tools with reasoning_effort ("Function tools with reasoning_effort
# are not supported ... use /v1/responses"), so every hermes call with tools
# enabled 400s. Setting api_mode explicitly (read directly from model.api_mode
# for api_key-auth providers, per hermes_cli.runtime_provider) makes hermes
# speak Responses API regardless of hostname.
#
# Both keys are skipped if a config already exists so a hand-customized
# provider/api_mode choice is never overwritten.
hermes_config_dir="${HOME}/.hermes"
hermes_config="${hermes_config_dir}/config.yaml"
if [ ! -f "${hermes_config}" ]; then
  mkdir -p "${hermes_config_dir}"
  cat > "${hermes_config}" <<'YAML'
model:
  provider: openai-api
  api_mode: codex_responses
YAML
fi

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

# EXTRA_NPX_TOOLS is the npm/npx analog of EXTRA_UV_TOOLS: same one-off/
# custom-need opt-in for a single workspace, for npm-distributed CLIs instead
# of PyPI ones. Space-separated `npm install -g` targets, always
# version-pinned using npm's native `pkg@x.y.z` syntax (no `uv`-style `==`
# translation needed) for the same reason EXTRA_UV_TOOLS pins: this path is
# best-effort and untested by CI, so an unpinned entry can silently resolve
# to a different release on the next pod boot. Lands as an SSM param under
# this workspace's slug
# (/agentfarm/development/agentrunner/<slug>/EXTRA_NPX_TOOLS) and arrives
# here the same way EXTRA_UV_TOOLS does, via the existing ExternalSecret
# sweep — no image change, no per-workspace Dockerfile.
#
# `npm install -g` ordinarily fails EACCES for the non-root `agent` user
# because the bundled npm CLIs (claude/codex/opencode/pi) are installed as
# root before `USER agent` in agent-runtime-base/Dockerfile, leaving npm's
# default global prefix root-owned. That Dockerfile now redirects npm's
# global prefix to an agent-owned dir (`NPM_CONFIG_PREFIX=~/.npm-global`,
# its `bin/` already on `PATH` via the base image's own `ENV PATH`, so no
# export is needed here the way there is for `uv`) specifically so this
# install works unmodified for the agent user — see that Dockerfile's
# "Redirect npm's global-install prefix" comment. Best-effort: a failed
# install logs a warning and does not block pod boot, same as
# EXTRA_UV_TOOLS.
if [ -n "${EXTRA_NPX_TOOLS:-}" ]; then
  echo "entrypoint: installing extra npm tools: ${EXTRA_NPX_TOOLS}"
  for tool in ${EXTRA_NPX_TOOLS}; do
    npm install -g "${tool}" || echo "entrypoint: WARNING failed to install extra npm tool '${tool}' (continuing)" >&2
  done
fi

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

# ── Codex LLM proxy config ────────────────────────────────────────────────────
# Codex has no env-var override for its built-in OpenAI provider — only a
# config.toml one (see agent-runtime-base/README.md's "LLM proxy routing").
# agent-runtime-base/Dockerfile seeds /home/agent/.codex/config.toml with the
# llmproxy provider at build time, but gitops/base/agent-runtime/storage.yaml
# mounts an EFS PVC over the *entire* /home/agent, which shadows that image
# layer on every boot — the seed never actually reaches disk here. What does
# create ~/.codex/config.toml on first boot is `git-ai install-hooks` below,
# which writes only its own [features]/[[hooks.*]] tables, leaving Codex with
# no model_provider and stuck on its default OpenAI auth flow (prompts for an
# auth method even with OPENAI_API_KEY set). Self-heal here, before git-ai
# runs, the same way ~/.multica/config.json is rewritten unconditionally above
# instead of relying on a one-time image COPY the PVC can defeat.
#
# model_provider is a bare top-level TOML key: it must appear before the file's
# first [table] header (git-ai's [features]) or a TOML parser silently attaches
# it to whichever table is last in the file instead of the document root — so
# it's prepended, never appended.
codex_config="${HOME}/.codex/config.toml"
mkdir -p "${HOME}/.codex"
touch "${codex_config}"
if ! grep -q '^model_provider' "${codex_config}"; then
  codex_config_tmp="$(mktemp)"
  { printf 'model_provider = "openai_http"\n\n'; cat "${codex_config}"; } > "${codex_config_tmp}"
  mv "${codex_config_tmp}" "${codex_config}"
fi
if ! grep -q '^\[model_providers.openai_http\]' "${codex_config}"; then
  cat >> "${codex_config}" <<'TOML'

[model_providers.openai_http]
base_url = "https://llmproxy.g2.com/v1"
name = "OpenAI HTTP only"
env_key = "OPENAI_API_KEY"
supports_websockets = false
wire_api = "responses"
TOML
fi

# ── git-ai setup ──────────────────────────────────────────────────────────────
# git-ai is baked into the image at /usr/local/bin/git-ai but its user config
# dir (~/.git-ai/) lives on the work PVC, so setup must happen here at runtime.
# Creates:
#   ~/.git-ai/bin/git-ai  → /usr/local/bin/git-ai  (canonical user-path symlink)
#   ~/.git-ai/bin/git     → /usr/local/bin/git-ai  (PATH-based git interception)
#   ~/.git-ai/bin/git-og  → /usr/bin/git            (real git for git-ai internals)
#   ~/.git-ai/config.json (git_path + feature_flags; skipped if already present)
# Then registers Claude Code PreToolUse/PostToolUse hooks so agent commits are
# attributed in refs/notes/ai. Idempotent across pod restarts.
git_ai_bin="${HOME}/.git-ai/bin"
mkdir -p "${git_ai_bin}"
ln -sf /usr/local/bin/git-ai "${git_ai_bin}/git-ai" 2>/dev/null || true
ln -sf /usr/local/bin/git-ai "${git_ai_bin}/git"    2>/dev/null || true
ln -sf /usr/bin/git           "${git_ai_bin}/git-og" 2>/dev/null || true
if [ ! -f "${HOME}/.git-ai/config.json" ]; then
  printf '{\n  "git_path": "/usr/bin/git",\n  "feature_flags": {"async_mode": true}\n}\n' \
    > "${HOME}/.git-ai/config.json"
fi
git-ai install-hooks 2>/dev/null \
  || echo "entrypoint: WARNING git-ai install-hooks failed — AI attribution hooks may not be active" >&2

# ── Run daemon in foreground, draining in-flight tasks before shutdown ────────
# Not exec'd: the daemon's own SIGTERM handling cancels an in-flight task's
# runCtx immediately (it's derived from the same root ctx the signal cancels),
# so a mid-run agent session gets killed within seconds on a deploy/pod delete
# (AIPLAT-168). Running the daemon as a background child makes this script
# PID 1, so it receives SIGTERM first and can hold it until the daemon reports
# no active tasks (or a capped wait elapses) before forwarding it.
multica daemon start --foreground --device-name "${DEVICE_NAME}" &
DAEMON_PID=$!

drain_and_forward() {
  echo "entrypoint: SIGTERM received, draining active tasks before signaling daemon..." >&2
  local waited=0 max_wait="${DRAIN_MAX_SECONDS:-570}" interval=5 active
  while (( waited < max_wait )); do
    active=$(multica daemon status --output json 2>/dev/null | jq -r '.active_task_count // 0' 2>/dev/null || echo 1)
    [[ "${active}" == "0" ]] && break
    sleep "${interval}"
    waited=$((waited + interval))
  done
  echo "entrypoint: forwarding SIGTERM to daemon (waited ${waited}s)" >&2
  kill -TERM "${DAEMON_PID}" 2>/dev/null || true
}
trap drain_and_forward SIGTERM SIGINT
wait "${DAEMON_PID}"

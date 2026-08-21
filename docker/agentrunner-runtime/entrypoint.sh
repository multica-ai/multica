#!/bin/bash
set -euo pipefail

# ── Mandatory env ─────────────────────────────────────────────────────────────
: "${MULTICA_PAT:?MULTICA_PAT required}"
: "${MULTICA_WORKSPACE_ID:?MULTICA_WORKSPACE_ID required}"
: "${ANTHROPIC_API_KEY:?ANTHROPIC_API_KEY required}"
: "${OPENAI_API_KEY:?OPENAI_API_KEY required}"
: "${WORKSPACE_SLUG:?WORKSPACE_SLUG required}"

# ── Secrets dir (tmpfs) ────────────────────────────────────────────────────────
# Backed by a tmpfs (`emptyDir: {medium: Memory}`) volume mounted in
# gitops/base/agent-runtime/deployment.yaml, so material written here lives in
# RAM for the pod's lifetime and is never written to the node's disk. Defined
# early so both the GitHub App ID bridge below and the PEM-secret bridge
# further down can share it.
SECRETS_DIR="${HOME}/.secrets"
mkdir -p "${SECRETS_DIR}"
# ${SECRETS_DIR} is the mount point of the emptyDir{medium: Memory} volume in
# gitops/base/agent-runtime/deployment.yaml, created root-owned with group
# 1000 via fsGroup. fsGroup grants the non-root agent user rw access but not
# chmod (that needs ownership or CAP_FOWNER), so this fails with EPERM —
# harmless, since fsGroup already restricts the dir to owner/group only.
chmod 700 "${SECRETS_DIR}" 2>/dev/null || true

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

  # Hermes tool-command subprocesses inherit GITHUB_APP_PRIVATE_KEY but not
  # GITHUB_APP_ID (the two arrive through different forwarding paths and only
  # the key survives into that subprocess env), so the gh wrapper's own
  # precedence check — which requires both — used to fall through to the real
  # gh binary with no GH_TOKEN and GitHub CLI commands failed authentication.
  # GITHUB_APP_ID isn't secret (it's a public numeric identifier, not
  # credential material), so write it to the tmpfs secrets dir here;
  # gh-platform-bot-wrapper.sh reads it back as a fallback when its own
  # environment lacks it (PRO-68).
  printf '%s' "${GITHUB_APP_ID}" > "${SECRETS_DIR}/GITHUB_APP_ID"
  chmod 600 "${SECRETS_DIR}/GITHUB_APP_ID"
fi

# ── Git identity ──────────────────────────────────────────────────────────────
# Commits from runner agents are bot-authored. Defaults target the Platform Bot
# and are overridable via the deployment env for precise attribution.
git config --global user.name  "${GIT_USER_NAME:-g2-platform-bot[bot]}"
git config --global user.email "${GIT_USER_EMAIL:-g2-platform-bot[bot]@users.noreply.github.com}"

# ── Git ownership check ───────────────────────────────────────────────────────
# $HOME is an EFS access point, so files created under it come back owned by an
# EFS-mapped uid rather than the image's `agent` user (uid 1000), and the
# fix-efs-permissions init container deliberately chowns top-level dirs only —
# recursing across the real checkouts under multica_workspaces would scale with
# repo size and slow every pod start. Access still works (the access point
# enforces its own identity), but git compares st_uid against geteuid() and
# refuses on inequality, so every agent-invoked git command in a fresh checkout
# died with "detected dubious ownership" until the agent hand-added an exception.
#
# The daemon already does this for its own git subprocesses — see gitEnv() in
# server/internal/daemon/repocache/cache.go — and the same reasoning applies to
# agent-invoked git: single-tenant pod, one uid, nobody else can plant a repo,
# so the check buys nothing here. Note that git supports only exact paths or a
# literal `*`; a `/home/agent/multica_workspaces/*` prefix glob does NOT match.
#
# --replace-all rather than --add so pods with an accumulated (and duplicated)
# list of per-checkout exceptions, added by agents working around this, collapse
# to the single canonical entry on next start instead of growing forever.
git config --global --replace-all safe.directory '*'

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
# "where did this secret come from" (ROIPPC-2). Uses `${SECRETS_DIR}`, set up
# near the top of this script.
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

# A pod stopped with SIGKILL leaves daemon.pid behind on the EFS-backed $HOME.
# Nothing in the normal pod lifecycle reads it — the "already running" guard is
# the loopback health-port bind and `daemon status` queries that port — but PIDs
# restart from 1 in a fresh container, so a stale entry can name a live and
# entirely unrelated process that `multica daemon stop` would then kill.
rm -f "${config_dir}/daemon.pid"

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

# ── Fan out ~/.agents/skills into each runtime's user-level skill root ─────────
# ~/.agents/skills is a Multica *import* source, not an execution path: the
# daemon lists it as localSkillRootUniversal (server/internal/daemon/
# local_skills.go) so the UI can import a runtime-local skill into the
# workspace registry, but no CLI reads it at task time. Registry skills reach a
# task only once they are bound to an agent, which freezes them at import and
# drops the `git pull` freshness the seed above exists to provide.
#
# So the seeded skills are copied into each installed runtime's own user-level
# root, which every CLI reads natively:
#   claude → ~/.claude/skills        pi     → ~/.pi/agent/skills
#   codex  → ~/.codex/skills         hermes → ~/.hermes/skills
# Codex and Hermes redirect HOME per task, but the daemon already replays these
# shared roots into the per-task home — seedUserCodexSkills copies
# ~/.codex/skills into CODEX_HOME, and hermesExternalSkillRoots references
# ~/.hermes/skills last (both in server/internal/daemon/execenv/). In both the
# shared root yields to workspace-bound skills on a name collision, so fanning
# out here cannot shadow a skill an agent is explicitly bound to.
#
# opencode is installed too but is deliberately NOT in the list: it is the one
# runtime that already reads ~/.agents/skills itself. It scans
# ~/.claude/skills/**/SKILL.md and ~/.agents/skills/**/SKILL.md as "external
# skills", globstarred, so it picks the seeded tree up at its native
# <category>/<skill> depth with no flattening. Verified with
# `opencode debug skill`: 37 skills resolved from ~/.agents/skills before this
# fan-out existed. Adding ~/.config/opencode/skills would copy 37 directories
# per boot to surface skills opencode can already see. opencode dedupes by skill
# name, so the ~/.claude/skills copy below does not double-list them either.
# (Its scan of ~/.claude/ and ~/.agents/ is disabled by
# OPENCODE_DISABLE_EXTERNAL_SKILLS=1 / OPENCODE_DISABLE_CLAUDE_CODE_SKILLS=1 —
# if either is ever set for opencode tasks, add its root here.)
#
# Two shape mismatches to reconcile:
#   - ai-enhancement-hub nests one level deeper than any runtime expects
#     (<category>/<skill>/SKILL.md vs <root>/<skill>/SKILL.md), so the category
#     level is dropped. The daemon's own discovery handles nesting to
#     maxLocalSkillDirDepth and is unaffected either way.
#   - gitops/base/agent-runtime/storage.yaml mounts an EFS PVC over all of
#     /home/agent, so these roots persist across boots. The fan-out must be
#     rebuilt each boot, not appended to, or a skill deleted upstream lingers
#     forever.
if [ "${AGENTRUNNER_SKILL_FANOUT:-1}" != "0" ]; then
  echo "agentrunner: fanning out skills to runtime discovery paths..."
  skill_roots="${HOME}/.claude/skills ${HOME}/.codex/skills ${HOME}/.pi/agent/skills ${HOME}/.hermes/skills"
  # Marker file identifying a directory this fan-out owns. Only marked
  # directories are cleared, so hand-installed skills and a runtime's own
  # built-ins (e.g. ~/.codex/skills/.system) survive.
  fanout_marker=".agentrunner-fanout"

  for root in ${skill_roots}; do
    mkdir -p "${root}"
    find "${root}" -mindepth 1 -maxdepth 1 -type d \
      -exec test -f "{}/${fanout_marker}" \; -print0 2>/dev/null |
      xargs -0 -r rm -rf
  done

  fanout_total=0
  fanout_skipped=0
  # -mindepth 2 skips a stray SKILL.md at the root of the tree; -maxdepth 4
  # allows a category level or two below that without descending arbitrarily.
  while IFS= read -r skill_md; do
    # Guard the empty line: with no matches `find` yields nothing, and an
    # unguarded read would still enter the loop once with an empty value,
    # resolving to dirname "" = "." and reporting a phantom skill.
    [ -n "${skill_md}" ] || continue
    skill_dir="$(dirname "${skill_md}")"
    skill_name="$(basename "${skill_dir}")"
    for root in ${skill_roots}; do
      dest="${root}/${skill_name}"
      if [ -e "${dest}" ]; then
        # Either two categories carry the same skill name, or the name is
        # already taken by a hand-installed skill. Both are the operator's
        # call to resolve upstream; first writer wins and we say so loudly.
        echo "agentrunner: skill name collision, keeping existing ${dest}" >&2
        fanout_skipped=$((fanout_skipped + 1))
        continue
      fi
      cp -r "${skill_dir}" "${dest}"
      touch "${dest}/${fanout_marker}"
    done
    fanout_total=$((fanout_total + 1))
  done <<EOF
$(find "${HOME}/.agents/skills" -mindepth 2 -maxdepth 4 -name SKILL.md | sort)
EOF

  echo "agentrunner: fanned out ${fanout_total} skills to 4 runtime roots (${fanout_skipped} collisions skipped)"
fi

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
# Rewritten unconditionally, not just when absent: a persisted config from a
# prior boot, an older entrypoint version, or a hand edit can carry a
# model_provider/openai_http block that no longer points at the proxy, and a
# presence-only check leaves that silently in place instead of correcting it.
#
# model_provider is a bare top-level TOML key: it must appear before the file's
# first [table] header (git-ai's [features]) or a TOML parser silently attaches
# it to whichever table is last in the file instead of the document root — so
# any existing occurrence is stripped and it's prepended fresh, never appended.
# [model_providers.openai_http] is stripped as a whole block (its header
# through the next top-level [table] header or EOF) and re-appended canonically
# so a stale base_url can't survive under a differently-shaped table.
codex_config="${HOME}/.codex/config.toml"
mkdir -p "${HOME}/.codex"
touch "${codex_config}"
codex_config_tmp="$(mktemp)"
{
  printf 'model_provider = "openai_http"\n\n'
  # `grep -v` exits 1 when it selects zero lines (e.g. an empty file on first
  # boot); under `pipefail` that would abort the whole entrypoint, so the
  # group swallows that specific non-match case before piping into awk.
  { grep -v '^model_provider' "${codex_config}" || true; } | awk '
    /^\[model_providers\.openai_http\]/ { skip=1; next }
    /^\[/ { skip=0 }
    skip { next }
    { print }
  '
  cat <<'TOML'

[model_providers.openai_http]
base_url = "https://llmproxy.g2.com/v1"
name = "OpenAI HTTP only"
env_key = "OPENAI_API_KEY"
supports_websockets = false
wire_api = "responses"
TOML
} > "${codex_config_tmp}"
mv "${codex_config_tmp}" "${codex_config}"

# ── Pi LLM proxy extension ────────────────────────────────────────────────────
# Pi has no *_BASE_URL env var; pi.registerProvider() — an extension file
# auto-discovered from ~/.pi/agent/extensions/ at startup — is the only
# override (see docker/agent-runtime-base/llmproxy/pi/llmproxy.ts). Same
# EFS-shadow problem as Codex above: agent-runtime-base/Dockerfile COPYs this
# file into the image, but storage.yaml's PVC over all of /home/agent means
# that image copy never reaches disk on a real pod boot. Rewritten
# unconditionally on every boot for the same reason as the Codex config above.
# This file is entirely self-managed (agents have no reason to hand-edit a
# provider-registration extension), so there's no hand-customized state to
# preserve — a plain overwrite is safe and simplest.
pi_ext_dir="${HOME}/.pi/agent/extensions"
mkdir -p "${pi_ext_dir}"
cat > "${pi_ext_dir}/llmproxy.ts" <<'TS'
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

const LLM_PROXY = "https://llmproxy.g2.com";

export default function (pi: ExtensionAPI) {
  pi.registerProvider("anthropic", {
    baseUrl: LLM_PROXY,
  });

  pi.registerProvider("openai", {
    baseUrl: `${LLM_PROXY}/v1`,
  });
}
TS

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

# ── Run daemon in foreground, stopping it recoverably on shutdown ─────────────
# tini is PID 1 and forwards SIGTERM to this script, its only child. The daemon
# is a background child rather than exec'd so the trap below can choose HOW to
# stop it — and that choice decides whether an interrupted agent session is
# recoverable, because the daemon's two stop paths are not equivalent:
#
#   SIGTERM — the daemon cancels the in-flight run's ctx (derived from the same
#     root ctx the signal cancels) and reports the task terminal with
#     failure_reason="cancelled". The server's auto-retry path does not treat
#     that reason as retryable, so the task dies and its progress is lost.
#   SIGKILL — the daemon reports nothing, so the task stays `running`
#     server-side. The replacement pod's daemon calls recover-orphans during
#     startup, which reclaims it as failure_reason="runtime_recovery" — that IS
#     retryable, and the retry inherits session_id + work_dir. The agent then
#     resumes its provider session (`claude --resume`) in the same workdir,
#     which is still there because $HOME is the EFS-backed PVC.
#
# So: drain first, and stop cleanly with SIGTERM only when the daemon positively
# reports itself idle (that lets it deregister its runtimes and flush). If work
# is still in flight — or we cannot tell — use SIGKILL, the recoverable stop,
# rather than spending the session on a tidy-looking shutdown that discards it.
multica daemon start --foreground --device-name "${DEVICE_NAME}" &
DAEMON_PID=$!

# Echoes the daemon's in-flight task count, or fails if that count cannot be
# trusted. Gating on status=="running" is load-bearing: an unreachable health
# endpoint still exits 0 with {"status":"stopped"} and no active_task_count, so
# a bare `.active_task_count // 0` reads a wedged daemon as idle.
active_task_count() {
  local health count
  health=$(multica daemon status --output json 2>/dev/null) || return 1
  count=$(jq -r 'select(.status == "running") | .active_task_count // 0' <<<"${health}" 2>/dev/null) || return 1
  [[ "${count}" =~ ^[0-9]+$ ]] || return 1
  printf '%s' "${count}"
}

drain_and_stop() {
  local drain_max="${DRAIN_MAX_SECONDS:-120}"
  local stop_wait="${DAEMON_STOP_WAIT_SECONDS:-30}"
  local waited=0 interval=5 probe_failures=0 active=""
  local max_probe_failures=3

  echo "entrypoint: SIGTERM received, draining active tasks (max ${drain_max}s)..." >&2
  while :; do
    if active=$(active_task_count); then
      probe_failures=0
      if [[ "${active}" == "0" ]]; then break; fi
    else
      active=""
      probe_failures=$((probe_failures + 1))
      # Don't spend the drain budget waiting on a daemon that isn't answering;
      # bail out early and take the recoverable stop below.
      if (( probe_failures >= max_probe_failures )); then
        echo "entrypoint: daemon status unreadable ${probe_failures}x, cannot confirm idle" >&2
        break
      fi
    fi
    if (( waited >= drain_max )); then break; fi
    sleep "${interval}"
    waited=$((waited + interval))
  done

  if [[ "${active}" == "0" ]]; then
    echo "entrypoint: idle after ${waited}s, stopping daemon cleanly (SIGTERM)" >&2
    kill -TERM "${DAEMON_PID}" 2>/dev/null || true
  else
    echo "entrypoint: work still in flight after ${waited}s (active=${active:-unknown}); SIGKILL so the server reclaims it as a recoverable orphan" >&2
    kill -KILL "${DAEMON_PID}" 2>/dev/null || true
  fi

  # A trapped signal makes the `wait` at the bottom of this script return
  # immediately, so without reaping here the script would fall off the end while
  # the daemon is still shutting down, taking tini — and therefore the container
  # — with it. `wait` rather than a `kill -0` poll because a killed-but-unreaped
  # child stays visible to `kill -0` as a zombie, which reads as "still alive".
  # The watchdog bounds it, so a wedged daemon cannot hold the pod past
  # terminationGracePeriodSeconds.
  (
    local ticks=0
    while (( ticks < stop_wait )); do
      sleep 1
      if ! kill -0 "${DAEMON_PID}" 2>/dev/null; then exit 0; fi
      ticks=$((ticks + 1))
    done
    echo "entrypoint: daemon did not exit within ${stop_wait}s, escalating to SIGKILL" >&2
    kill -KILL "${DAEMON_PID}" 2>/dev/null || true
  ) &
  wait "${DAEMON_PID}" 2>/dev/null || true
}
trap drain_and_stop SIGTERM SIGINT
wait "${DAEMON_PID}"

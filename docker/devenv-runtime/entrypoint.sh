#!/bin/bash
set -euo pipefail

OPENCODE_PID=""
WORKSPACES="${HOME}/workspaces"

cleanup() {
  if [ -n "${OPENCODE_PID}" ] && kill -0 "${OPENCODE_PID}" 2>/dev/null; then
    kill "${OPENCODE_PID}" 2>/dev/null
    wait "${OPENCODE_PID}" 2>/dev/null
  fi
  exit 0
}

restart_opencode() {
  if [ -n "${OPENCODE_PID}" ] && kill -0 "${OPENCODE_PID}" 2>/dev/null; then
    echo "[entrypoint] Restarting opencode..."
    pkill -9 -f '.opencode serve' 2>/dev/null || true
  fi
}

trap cleanup SIGTERM SIGINT
trap restart_opencode SIGUSR1

# ── GitHub credential helper ─────────────────────────────────────────────────
if [ -n "${GH_TOKEN:-}" ]; then
  if gh auth setup-git --hostname github.com; then
    git config --global --unset-all url."https://github.com/".insteadOf 2>/dev/null || true
    git config --global --add url."https://github.com/".insteadOf "git@github.com:"
    git config --global --add url."https://github.com/".insteadOf "ssh://git@github.com/"
  fi
fi

# ── Git identity ─────────────────────────────────────────────────────────────
if [ -n "${GIT_USER_NAME:-}" ]; then git config --global user.name "$GIT_USER_NAME"; fi
if [ -n "${GIT_USER_EMAIL:-}" ]; then git config --global user.email "$GIT_USER_EMAIL"; fi

# ── SSH key ──────────────────────────────────────────────────────────────────
generate_ssh_key() {
  SSH_DIR="${HOME}/.ssh"
  SSH_KEY="${SSH_DIR}/id_ed25519"
  mkdir -p "${SSH_DIR}"
  chmod 700 "${SSH_DIR}" 2>/dev/null || true
  if [ ! -f "${SSH_KEY}" ]; then
    ssh-keygen -t ed25519 -N "" -f "${SSH_KEY}" -C "${GIT_USER_EMAIL:-agent@devenv}" >/dev/null
    echo "[entrypoint] Generated SSH key"
  fi
  chmod 600 "${SSH_KEY}" 2>/dev/null || true
  chmod 644 "${SSH_KEY}.pub" 2>/dev/null || true
}
generate_ssh_key

# ── LiteLLM auth ─────────────────────────────────────────────────────────────
if [ -n "${LITELLM_API_KEY:-}" ]; then
  AUTH_FILE="${HOME}/.local/share/opencode/auth.json"
  mkdir -p "$(dirname "${AUTH_FILE}")"
  printf '{"litellm":{"type":"api","key":"%s"}}' "${LITELLM_API_KEY}" > "${AUTH_FILE}"
  echo "[entrypoint] Generated auth.json for litellm"
fi

# ── Source user env (persistent volume) ──────────────────────────────────────
if [ -f "${WORKSPACES}/.opencode-env" ]; then
  . "${WORKSPACES}/.opencode-env"
  grep -q 'opencode-env' "${HOME}/.bashrc" 2>/dev/null || echo ". ${WORKSPACES}/.opencode-env" >> "${HOME}/.bashrc" 2>/dev/null || true
  export BASH_ENV="${WORKSPACES}/.opencode-env"
fi

# ── Clone repo ───────────────────────────────────────────────────────────────
clone_repo() {
  REPO="${GITHUB_REPO:-}"
  if [ -z "${REPO}" ]; then return; fi

  CLONE_URL="https://github.com/${REPO}.git"
  TARGET="${WORKSPACES}/$(echo "${REPO}" | sed 's|.*/||')"

  if [ -d "${TARGET}/.git" ]; then
    git config --global --add safe.directory "${TARGET}"
    git -C "${TARGET}" pull --ff-only 2>/dev/null || true
    return
  fi

  mkdir -p "${TARGET}"
  git config --global --add safe.directory "${TARGET}"
  git clone --depth 1 "${CLONE_URL}" "${TARGET}" 2>/dev/null || {
    echo "[entrypoint] WARNING: Failed to clone ${REPO} (non-fatal)"
  }
}
clone_repo

# ── Clone ai-enhancement-hub ────────────────────────────────────────────────
clone_ai_hub() {
  DEST="${HOME}/.local/share/ai-enhancement-hub"
  if [ -d "${DEST}/.git" ]; then
    git -C "${DEST}" pull --ff-only 2>/dev/null || true
    return
  fi
  git clone --depth 1 "https://github.com/g2crowd/ai-enhancement-hub.git" "${DEST}" 2>/dev/null || {
    echo "[entrypoint] WARNING: Failed to clone ai-enhancement-hub (non-fatal)"
  }
}
clone_ai_hub

# ── Link ai-hub resources ───────────────────────────────────────────────────
link_ai_hub() {
  HUB="${HOME}/.local/share/ai-enhancement-hub/shared/opencode"
  if [ ! -d "${HUB}" ]; then return; fi
  for SUBDIR in agents commands skills; do
    [ -d "${HUB}/${SUBDIR}" ] || continue
    mkdir -p "${WORKSPACES}/.opencode/${SUBDIR}"
    for FILE in "${HUB}/${SUBDIR}"/*.md; do
      [ -f "${FILE}" ] || continue
      BASENAME="${FILE##*/}"
      [ "${BASENAME}" = "README.md" ] && continue
      TARGET="${WORKSPACES}/.opencode/${SUBDIR}/${BASENAME}"
      [ -e "${TARGET}" ] && continue
      ln -s "${FILE}" "${TARGET}"
    done
  done
}
link_ai_hub

# ── Seed AGENTS.md ───────────────────────────────────────────────────────────
if [ ! -f "${WORKSPACES}/AGENTS.md" ] && [ -f /opt/AGENTS.md ]; then
  cp /opt/AGENTS.md "${WORKSPACES}/AGENTS.md"
fi

# ── Agentfarm mode ───────────────────────────────────────────────────────────
if [ -n "${MULTICA_PAT:-}" ] \
  && [ -n "${MULTICA_WORKSPACE_ID:-}" ] \
  && [ -n "${LITELLM_API_KEY:-}" ] \
  && [ -n "${WORKSPACE_SLUG:-}" ]; then
  echo "devenv: agentfarm mode — running agentfarm-bootstrap.sh"
  /usr/local/bin/agentfarm-bootstrap.sh

elif [ -n "${MULTICA_TOKEN:-}" ]; then
  server_url="${MULTICA_SERVER_URL:-https://agentfarm.g2.com}"
  app_url="${MULTICA_APP_URL:-https://agentfarm.g2.com}"
  config_dir="$HOME/.multica"
  mkdir -p "$config_dir"
  umask 077
  cat > "$config_dir/config.json" <<JSON
{
  "server_url": "${server_url}",
  "app_url": "${app_url}",
  "token": "${MULTICA_TOKEN}"
}
JSON
  multica auth status || true
  multica daemon start &
  echo "devenv: multica daemon started (pid $!)"
fi

install_arb() {
  if command -v arb >/dev/null 2>&1; then return; fi
  if ! gh auth status >/dev/null 2>&1; then return; fi
  echo "@g2crowd:registry=https://npm.pkg.github.com" > /tmp/.npmrc
  echo "//npm.pkg.github.com/:_authToken=$(gh auth token)" >> /tmp/.npmrc
  HOME=/tmp npm install -g @g2crowd/arb-cli@latest 2>/dev/null || {
    echo "[entrypoint] WARNING: Failed to install arb (non-fatal)"
    rm -f /tmp/.npmrc
    return
  }
  rm -f /tmp/.npmrc
  echo "[entrypoint] Installed arb"
}
install_arb

configure_arb() {
  if [ -z "${ARB_TOKEN:-}" ]; then return; fi

  ARB_STATE="${HOME}/.local/share/devenv/arb"
  RUNTIME_CONFIG="${ARB_STATE}/arb.json"
  mkdir -p "${ARB_STATE}"

  if [ -f "${RUNTIME_CONFIG}" ]; then
    echo "[entrypoint] arb config exists — preserving user edits"
    return
  fi

  JIRA_EMAIL="${GIT_USER_EMAIL:-}"
  cat > "${RUNTIME_CONFIG}" <<MONEOF
{
  "org": "g2crowd",
  "reposDir": "${WORKSPACES}",
  "arbServerUrl": "${ARB_URL:-https://arb.g2.com}",
  "arbServerToken": "${ARB_TOKEN}",
  "intervalMs": 60000,
  "triggerPhrases": ["AI:"],
  "opencodeUrl": "http://localhost:4096",
  "jiraWorkingDir": "${WORKSPACES}",
  "jira": {
    "baseUrl": "https://g2crowd.atlassian.net",
    "email": "${JIRA_EMAIL}"
  }
}
MONEOF
  echo "[entrypoint] Generated initial arb config"
}
configure_arb

# ── Warm oh-my-openagent plugin cache ────────────────────────────────────────
warm_plugin_cache() {
  PKGS="${HOME}/.cache/opencode/packages"
  mkdir -p "${PKGS}"
  printf '{"dependencies":{"oh-my-openagent":"latest"}}' > "${PKGS}/package.json"
  (cd "${PKGS}" && bun install) 2>/dev/null
  echo "[entrypoint] Warmed plugin cache"
}
warm_plugin_cache

echo "[entrypoint] Initialization complete"

# ── Start arb ────────────────────────────────────────────────────────────────
start_arb() {
  if [ -z "${ARB_TOKEN:-}" ]; then return; fi
  ARB_STATE="${HOME}/.local/share/devenv/arb"
  RUNTIME_CONFIG="${ARB_STATE}/arb.json"
  if [ ! -f "${RUNTIME_CONFIG}" ]; then return; fi

  (
    while ! nc -z localhost 4096 2>/dev/null; do sleep 1; done
    echo "[arb] OpenCode is up — starting poller and client"
    arb start --fresh --config "${RUNTIME_CONFIG}" >> "${ARB_STATE}/arb.log" 2>&1 &
    arb client --config "${RUNTIME_CONFIG}" >> "${ARB_STATE}/arb-client.log" 2>&1 &
  ) &
  echo "[entrypoint] arb will start once OpenCode is listening"
}
start_arb

# ── Start opencode with restart loop ─────────────────────────────────────────
host="${OPENCODE_HOST:-0.0.0.0}"
port="${OPENCODE_PORT:-4096}"

cd "${HOME}"
while true; do
  # shellcheck disable=SC2086
  opencode serve --hostname "$host" --port "$port" ${OPENCODE_EXTRA_ARGS:-} &
  OPENCODE_PID=$!
  wait "${OPENCODE_PID}" || true
  echo "[entrypoint] opencode exited — restarting in 2s..."
  sleep 2
done

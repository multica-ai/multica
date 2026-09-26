#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
guard="$root_dir/scripts/guard_compose_project.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

# --- 1. Outside a daemon task env root ---------------------------------------
# Plain cwd without task markers and without daemon env vars: the guard must
# pass through (production selfhost and normal dev compose runs are unaffected).
(
  cd "$tmp_dir"
  env -u MULTICA_DAEMON_PORT -u MULTICA_TASK_ID -u COMPOSE_PROJECT_NAME \
    "$guard" up -d >/dev/null 2>&1 || fail "guard refused a compose run from a non-task cwd"
)

# --- 2. Inside a daemon task env root, effective project == multica ----------
# Simulate a task worktree: daemon task env vars set, and a .managed_env.json
# marker in a parent dir. A bare `docker compose up` naming project "multica"
# must be refused.
(
  task_dir="$tmp_dir/multica_workspaces/ws/task-key/worktree"
  mkdir -p "$task_dir"
  printf '{"managed_by":"multica-daemon-managed-env"}' >"$tmp_dir/multica_workspaces/ws/task-key/.managed_env.json"
  cd "$task_dir"
  export MULTICA_DAEMON_PORT=19514
  export MULTICA_TASK_ID=sentinel-task-id
  unset COMPOSE_PROJECT_NAME 2>/dev/null || true
  printf 'name: multica\n' > docker-compose.yml

  if "$guard" up -d >/dev/null 2>&1; then
    fail "guard allowed a 'multica' project from inside a daemon task env root"
  fi

  # A non-multica project name must be allowed, even inside the task env root.
  export COMPOSE_PROJECT_NAME=multica-task-other
  if ! "$guard" up -d >/dev/null 2>&1; then
    fail "guard refused an isolated project name inside a task env root"
  fi
  unset COMPOSE_PROJECT_NAME
)

# --- 3. Inside a task env root, file has no name: -> default is dir basename --
(
  task_dir="$tmp_dir/workspaces/ws/task-2/worktree"
  mkdir -p "$task_dir"
  printf '{"managed_by":"multica-daemon-managed-env"}' >"$tmp_dir/workspaces/ws/task-2/.managed_env.json"
  cd "$task_dir"
  export MULTICA_DAEMON_PORT=19515
  export MULTICA_TASK_ID=sentinel-task-2
  unset COMPOSE_PROJECT_NAME 2>/dev/null || true
  # No `name:` line and default project = "worktree" (dir basename) → allowed.
  printf 'services:\n  x:\n    image: busybox\n' > docker-compose.yml

  if ! "$guard" up -d >/dev/null 2>&1; then
    fail "guard refused a dir-basename project inside a task env root"
  fi
)

# --- 4. -p / --project-name override wins -------------------------------------
(
  task_dir="$tmp_dir/workspaces/ws/task-3/worktree"
  mkdir -p "$task_dir"
  printf '{"managed_by":"multica-daemon-managed-env"}' >"$tmp_dir/workspaces/ws/task-3/.managed_env.json"
  cd "$task_dir"
  export MULTICA_DAEMON_PORT=19516
  export MULTICA_TASK_ID=sentinel-task-3
  unset COMPOSE_PROJECT_NAME 2>/dev/null || true
  printf 'name: multica\n' > docker-compose.yml

  # -p multica-task-abc must be allowed even though the file says `name: multica`.
  if ! "$guard" -p multica-task-abc up -d >/dev/null 2>&1; then
    fail "guard refused an explicit -p isolated project inside a task env root"
  fi
  # -p multica must be refused.
  if "$guard" -p multica up -d >/dev/null 2>&1; then
    fail "guard allowed an explicit -p multica inside a task env root"
  fi
)

echo "PASS: guard_compose_project tests"
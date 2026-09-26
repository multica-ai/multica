#!/usr/bin/env bash
# guard_compose_project.sh — refuse `docker compose` runs that would clobber the
# production `multica` stack from inside a multica daemon task.
#
# WHY
#   Repo worktrees carry the upstream `docker-compose.yml`, whose top-level
#   `name: multica` and bare `127.0.0.1:5432:5432` postgres bind match the
#   self-hosted production stack. A bare `docker compose up` inside a task
#   worktree therefore adopts the SAME project as production and tears the prod
#   stack down (2026-09-21 P0, RIC-947). The daemon already injects
#   COMPOSE_PROJECT_NAME=multica-task-<key> into every task env (Scheme A), so
#   ordinary task compose runs are isolated. This script is the belt-and-
#   suspenders guard for the case where an agent explicitly unsets or
#   overrides that variable back to `multica`.
#
# USAGE
#   scripts/guard_compose_project.sh           # check cwd for a dangerous project
#   scripts/guard_compose_project.sh up        # check then run `docker compose up`
#   scripts/guard_compose_project.sh exec ...  # same, any compose subcommand
#
# EXIT
#   0 — safe to proceed (not in a daemon task env, or the effective project name
#       is not `multica`)
#   1 — refused: would adopt the production project name from inside a task
#
# The production selfhost stack keeps `name: multica` and is NOT affected: it
# runs outside any daemon task env root, so this guard passes straight through.

set -euo pipefail

# --- effective project name resolution ---------------------------------------
# Precedence mirrors `docker compose`: -p / --project-name flag, then
# COMPOSE_PROJECT_NAME, then the top-level `name:` in the compose file.
# We only inspect what we can cheaply: env + the first matching file's `name:`.

resolve_effective_project() {
  # -p / --project-name on the command line wins.
  local prev=""
  for arg in "$@"; do
    if [ "$prev" = "-p" ] || [ "$prev" = "--project-name" ]; then
      printf '%s\n' "$arg"
      return 0
    fi
    case "$arg" in
      -p=*|--project-name=*)
        printf '%s\n' "${arg#*=}"
        return 0
        ;;
    esac
    prev="$arg"
  done

  # COMPOSE_PROJECT_NAME env wins over the file's `name:`.
  if [ -n "${COMPOSE_PROJECT_NAME:-}" ]; then
    printf '%s\n' "$COMPOSE_PROJECT_NAME"
    return 0
  fi

  # First compose file in the standard lookup order, then its `name:`.
  local f
  for f in docker-compose.yml docker-compose.yaml compose.yml compose.yaml \
      docker-compose.selfhost.yml docker-compose.selfhost.build.yml; do
    if [ -f "$f" ]; then
      local name
      name="$(sed -n 's/^name:[[:space:]]*//p' "$f" | head -n1)"
      if [ -n "$name" ]; then
        printf '%s\n' "$name"
        return 0
      fi
    fi
  done

  # No explicit project name — Docker defaults to the directory basename.
  printf '%s\n' "$(basename "$PWD")"
}

# --- daemon task detection ----------------------------------------------------
# A multica daemon task env root carries these signals:
#   * MULTICA_DAEMON_PORT + MULTICA_TASK_ID are set (daemon-spawned agent), and
#   * cwd lives under a dir containing .managed_env.json / .task_owner markers.
# The marker check is authoritative; env-only would over-trigger for a user who
# merely exported the variables.

is_daemon_task_cwd() {
  [ -n "${MULTICA_DAEMON_PORT:-}" ] && [ -n "${MULTICA_TASK_ID:-}" ] || return 1
  local d="$PWD"
  while [ "$d" != "/" ] && [ "$d" != "$HOME" ]; do
    if [ -f "$d/.managed_env.json" ] || [ -f "$d/.task_owner" ]; then
      return 0
    fi
    d="$(dirname "$d")"
  done
  return 1
}

main() {
  local project
  project="$(resolve_effective_project "$@")"

  if ! is_daemon_task_cwd; then
    # Not a daemon task — production selfhost and normal dev compose runs pass.
    echo "guard: outside a daemon task env root; project '${project}' allowed" >&2
    exit 0
  fi

  if [ "$project" = "multica" ]; then
    cat >&2 <<EOF
guard: REFUSED — 'docker compose ${*:-}' would use the production project name
'${project}' from inside a daemon task env root.

This worktree's compose file carries the upstream 'name: multica' (and a bare
127.0.0.1:5432:5432 postgres bind). Running it here would adopt the same Docker
Compose project as the self-hosted production stack and tear it down.

Run the task-local stack instead:
    COMPOSE_PROJECT_NAME=multica-task-\${MULTICA_TASK_ID} docker compose up -d
or, for a full production-like stack:
    docker compose -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml up -d   # from a REAL checkout, not a task worktree
EOF
    exit 1
  fi

  echo "guard: project '${project}' is isolated (not 'multica'); allowed" >&2
  exit 0
}

main "$@"

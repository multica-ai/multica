#!/usr/bin/env bash
# Keeps self-host checkouts from sharing one Docker Compose project (#7967).
#
# docker-compose.selfhost.yml pins `name: multica`, so every checkout that runs
# it lands in the same project: one postgres container, one pgdata volume. A
# second checkout with its own POSTGRES_PASSWORD then recreates the first
# checkout's database container and the first backend stops authenticating.
#
# Compose already lets COMPOSE_PROJECT_NAME (shell or .env) outrank `name:`,
# so existing installs keep `multica` and nothing about their volumes changes.
# This script only decides when a checkout needs its own name and refuses to
# touch a project another checkout owns.
#
#   selfhost-project.sh name       Print the project name Compose will use here.
#   selfhost-project.sh pick-name  Print a name for a brand new .env: `multica`
#                                  when nothing else uses it, otherwise one
#                                  derived from this checkout's path.
#   selfhost-project.sh check      Exit 1 if the project's containers were
#                                  created from a different directory.
#
# Ownership comes from the com.docker.compose.project.working_dir label Compose
# puts on every container. Volumes carry no such label, so `check` cannot tell
# whose leftover volume it is and lets it through; `pick-name` treats one as
# taken, because a fresh .env has a fresh password the old data would reject.
set -euo pipefail

DEFAULT_PROJECT=multica
DOCKER=${DOCKER:-docker}
ENV_FILE_PATH=${SELFHOST_ENV_FILE:-.env}

# Same precedence as Compose: environment, then the env file, then `name:`.
resolve_name() {
  local name=${COMPOSE_PROJECT_NAME:-}
  if [ -z "$name" ] && [ -f "$ENV_FILE_PATH" ]; then
    name=$(sed -n 's/^[[:space:]]*COMPOSE_PROJECT_NAME[[:space:]]*=[[:space:]]*//p' "$ENV_FILE_PATH" | tail -n 1)
    name=${name%$'\r'}
    name=${name#[\"\']}
    name=${name%[\"\']}
  fi
  printf '%s\n' "${name:-$DEFAULT_PROJECT}"
}

checkout_dir() {
  pwd -P
}

canonical_dir() {
  if [ -d "$1" ]; then (cd "$1" && pwd -P); else printf '%s\n' "$1"; fi
}

# Directories that own containers in project $1, one per line.
owner_dirs() {
  "$DOCKER" ps -a \
    --filter "label=com.docker.compose.project=$1" \
    --format '{{.Label "com.docker.compose.project.working_dir"}}' 2>/dev/null |
    sed '/^$/d' | sort -u
}

project_has_volumes() {
  [ -n "$("$DOCKER" volume ls -q --filter "label=com.docker.compose.project=$1" 2>/dev/null)" ]
}

foreign_owner() {
  local project=$1 here dir
  here=$(checkout_dir)
  while IFS= read -r dir; do
    [ -n "$dir" ] || continue
    if [ "$(canonical_dir "$dir")" != "$here" ]; then
      printf '%s\n' "$dir"
      return 0
    fi
  done < <(owner_dirs "$project")
  return 1
}

derived_name() {
  local here slug sum
  here=$(checkout_dir)
  slug=$(basename "$here" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9\n' '-' | sed 's/--*/-/g; s/^-//; s/-$//' | cut -c1-30)
  sum=$(printf '%s' "$here" | cksum | awk '{ printf "%08x", $1 }')
  printf '%s-%s-%s\n' "$DEFAULT_PROJECT" "${slug:-checkout}" "$sum"
}

suggest_fix() {
  local project=$1 owner=$2
  echo "Docker Compose project '$project' belongs to the self-host stack in:" >&2
  echo "  $owner" >&2
  echo "Running it from $(checkout_dir) would recreate that stack's containers." >&2
  echo "Either stop it from its own directory first, or give this checkout its" >&2
  echo "own project by adding this line to .env:" >&2
  echo "  COMPOSE_PROJECT_NAME=$(derived_name)" >&2
}

cmd=${1:-}
case "$cmd" in
name)
  resolve_name
  ;;
pick-name)
  if [ -n "${COMPOSE_PROJECT_NAME:-}" ]; then
    printf '%s\n' "$COMPOSE_PROJECT_NAME"
  elif foreign_owner "$DEFAULT_PROJECT" >/dev/null || project_has_volumes "$DEFAULT_PROJECT"; then
    derived_name
  else
    printf '%s\n' "$DEFAULT_PROJECT"
  fi
  ;;
check)
  project=$(resolve_name)
  if owner=$(foreign_owner "$project"); then
    suggest_fix "$project" "$owner"
    exit 1
  fi
  ;;
*)
  echo "usage: $0 name|pick-name|check" >&2
  exit 2
  ;;
esac

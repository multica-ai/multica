#!/usr/bin/env bash
# Resolve a local checkout path for a portfolio project (machine-specific; not in registry).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=lib/source-local-env.sh
source "$SCRIPT_DIR/lib/source-local-env.sh"

PROJECT_ID=""
REPO=""
QUIET=0

usage() {
  cat <<'EOF'
Usage: resolve-repo-path.sh (--id PROJECT_ID | --repo owner/name)

Resolves a local git checkout for portfolio dispatch. Paths are never committed
in project-registry.yaml — configure per machine via:

  1. .ai-company/config/local.env
     export AI_REPO_PATH_beatscape=/path/to/checkout
     export AI_REPO_PATH_landing_tool_a=/path/to/checkout   # hyphens → underscores
     export MUSIC_SAAS_PATH=/path/to/checkout   # beatscape alias

  2. Optional YAML map (gitignored):
     .ai-company/config/repo-paths.local.yaml

  3. Auto-discovery under AI_COMPANY_REPO_SEARCH (default: ~/Projects:~/Desktop:~)
     by matching `git remote get-url origin` to the GitHub repo.

Exit 0 prints the path; exit 1 if not found.
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --id) PROJECT_ID="${2:?}"; shift 2 ;;
    --repo) REPO="${2:?}"; shift 2 ;;
    --quiet) QUIET=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

if [ -z "$PROJECT_ID" ] && [ -z "$REPO" ]; then
  usage >&2
  exit 1
fi

repo_slug() {
  local raw="$1"
  raw="${raw#github.com/}"
  raw="${raw#https://github.com/}"
  echo "$raw"
}

env_key_for_repo() {
  local slug
  slug="$(repo_slug "$1")"
  echo "AI_REPO_PATH_${slug//\//_}"
}

env_key_for_id() {
  local id="${1//-/_}"
  echo "AI_REPO_PATH_$id"
}

path_if_dir() {
  local candidate="$1"
  if [ -n "$candidate" ] && [ -d "$candidate" ]; then
    echo "$candidate"
  fi
}

read_yaml_path() {
  local file="$MULTICA_ROOT/.ai-company/config/repo-paths.local.yaml"
  local key="$1"
  [ -f "$file" ] || return 0
  python3 - "$file" "$key" <<'PY'
import sys
from pathlib import Path

path = Path(sys.argv[1])
key = sys.argv[2]
if not path.is_file():
    sys.exit(0)
for line in path.read_text(encoding="utf-8").splitlines():
    line = line.split("#", 1)[0].strip()
    if not line or line.startswith("#"):
        continue
    if ":" not in line:
        continue
    k, v = line.split(":", 1)
    if k.strip() == key:
        print(v.strip().strip('"').strip("'"))
        break
PY
}

remote_matches_repo() {
  local dir="$1"
  local repo="$2"
  local remote
  remote="$(git -C "$dir" remote get-url origin 2>/dev/null || true)"
  [ -n "$remote" ] || return 1
  case "$remote" in
    *"github.com/${repo}"*|*"github.com:${repo}"*|*"github.com/${repo}.git"*)
      return 0
      ;;
  esac
  return 1
}

discover_repo_path() {
  local repo="$1"
  local search="${AI_COMPANY_REPO_SEARCH:-$HOME/Projects:$HOME/Desktop:$HOME}"
  local root candidate
  IFS=':' read -r -a roots <<<"$search"
  for root in "${roots[@]}"; do
    [ -d "$root" ] || continue
    for candidate in "$root"/*; do
      [ -d "$candidate/.git" ] || continue
      if remote_matches_repo "$candidate" "$repo"; then
        echo "$candidate"
        return 0
      fi
    done
  done
  return 1
}

registry_repo_for_id() {
  local id="$1"
  local registry="${REGISTRY:-$MULTICA_ROOT/.ai-company/templates/project-registry.yaml}"
  python3 - "$registry" "$id" <<'PY'
import sys
from pathlib import Path

registry = Path(sys.argv[1])
project_id = sys.argv[2]
if not registry.is_file():
    sys.exit(0)
current = None
for line in registry.read_text(encoding="utf-8").splitlines():
    stripped = line.strip()
    if stripped.startswith("- id:"):
        current = stripped.split(":", 1)[1].strip()
        continue
    if current == project_id and stripped.startswith("repo:"):
        repo = stripped.split(":", 1)[1].strip()
        print(repo.replace("github.com/", ""))
        break
PY
}

resolve_path() {
  local id="$1"
  local repo="$2"
  local candidate="" key=""

  if [ -z "$repo" ] && [ -n "$id" ]; then
    repo="$(registry_repo_for_id "$id" || true)"
  fi

  if [ -n "$id" ]; then
    key="$(env_key_for_id "$id")"
    candidate="${!key:-}"
    candidate="$(path_if_dir "$candidate")"
    [ -n "$candidate" ] && { echo "$candidate"; return 0; }

    candidate="$(path_if_dir "$(read_yaml_path "$id")")"
    [ -n "$candidate" ] && { echo "$candidate"; return 0; }

    if [ "$id" = "beatscape" ] && [ -n "${MUSIC_SAAS_PATH:-}" ]; then
      candidate="$(path_if_dir "$MUSIC_SAAS_PATH")"
      [ -n "$candidate" ] && { echo "$candidate"; return 0; }
    fi
  fi

  if [ -n "$repo" ]; then
    repo="$(repo_slug "$repo")"
    key="$(env_key_for_repo "$repo")"
    candidate="${!key:-}"
    candidate="$(path_if_dir "$candidate")"
    [ -n "$candidate" ] && { echo "$candidate"; return 0; }

    candidate="$(path_if_dir "$(read_yaml_path "$repo")")"
    [ -n "$candidate" ] && { echo "$candidate"; return 0; }

    if [ "$repo" = "chenzh/MusicSaas" ] && [ -n "${MUSIC_SAAS_PATH:-}" ]; then
      candidate="$(path_if_dir "$MUSIC_SAAS_PATH")"
      [ -n "$candidate" ] && { echo "$candidate"; return 0; }
    fi

    candidate="$(discover_repo_path "$repo" || true)"
    [ -n "$candidate" ] && { echo "$candidate"; return 0; }
  fi

  return 1
}

if path="$(resolve_path "$PROJECT_ID" "$REPO")"; then
  echo "$path"
  exit 0
fi

if [ "$QUIET" -eq 0 ]; then
  echo "error: no local checkout for id=${PROJECT_ID:-} repo=${REPO:-}" >&2
  echo "  set AI_REPO_PATH_<id> or MUSIC_SAAS_PATH in .ai-company/config/local.env" >&2
  echo "  or add .ai-company/config/repo-paths.local.yaml" >&2
fi
exit 1

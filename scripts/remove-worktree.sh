#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
worktree_input="${1:-}"

if [ -z "$worktree_input" ]; then
  echo "Usage: make remove-worktree WORKTREE=/path/to/worktree" >&2
  exit 1
fi

repo_root="$(git rev-parse --show-toplevel)"
if [[ "$worktree_input" = /* ]]; then
  candidate="$worktree_input"
else
  candidate="$PWD/$worktree_input"
fi

if [ ! -d "$candidate" ]; then
  echo "Worktree directory does not exist: $worktree_input" >&2
  exit 1
fi

target="$(cd "$candidate" && pwd -P)"
current_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
current="$(cd "$current_root" && pwd -P)"
primary=""
registered=0
locked=0
listed_path=""

while IFS= read -r line || [ -n "$line" ]; do
  case "$line" in
    worktree\ *)
      listed_path="${line#worktree }"
      if [ -z "$primary" ]; then
        primary="$listed_path"
      fi
      if [ "$listed_path" = "$target" ]; then
        registered=1
      fi
      ;;
    locked*)
      if [ "$listed_path" = "$target" ]; then
        locked=1
      fi
      ;;
  esac
done < <(git -C "$repo_root" worktree list --porcelain)

if [ "$registered" != "1" ]; then
  echo "Not a registered worktree of this repository: $target" >&2
  exit 1
fi

if [ "$target" = "$primary" ]; then
  echo "Refusing to remove the primary checkout: $target" >&2
  exit 1
fi

if [ "$target" = "$current" ]; then
  echo "Refusing to remove the current worktree from inside itself." >&2
  echo "Run this command from another checkout." >&2
  exit 1
fi

if [ "$locked" = "1" ]; then
  echo "Refusing to remove locked worktree: $target" >&2
  exit 1
fi

if [ -n "$(git -C "$target" status --porcelain --untracked-files=all)" ]; then
  echo "Refusing to remove dirty worktree before its database is dropped: $target" >&2
  echo "Commit, stash, or discard its changes first." >&2
  exit 1
fi

worktree_env="$target/.env.worktree"
if [ -f "$worktree_env" ]; then
  db_drop_status=0
  bash "$script_dir/drop-database.sh" "$worktree_env" || db_drop_status=$?
  case "$db_drop_status" in
    0) ;;
    2)
      echo "Worktree was not removed."
      exit 0
      ;;
    *) exit "$db_drop_status" ;;
  esac
else
  echo "No .env.worktree found; skipping database cleanup."
fi

git -C "$repo_root" worktree remove "$target"
echo "Removed worktree '$target'."

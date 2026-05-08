#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"

trim() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "$value"
}

is_dirty() {
  ! git -C "$REPO_ROOT" diff --quiet --ignore-submodules -- ||
    ! git -C "$REPO_ROOT" diff --cached --quiet --ignore-submodules --
}

derive_from_git() {
  local head best_tag best_distance best_base best_base_count tag distance parsed
  local base commit_count short_hash dirty_suffix

  head="$(git -C "$REPO_ROOT" rev-parse HEAD 2>/dev/null || true)"
  if [[ -z "$head" ]]; then
    printf 'dev'
    return
  fi

  best_tag=""
  best_distance=""
  best_base=""
  best_base_count=""
  while IFS= read -r tag; do
    parsed="$(TAG="$tag" python3 <<'PY'
import os
import re
import sys

tag = os.environ["TAG"]
match = re.fullmatch(r"(v\d+\.\d+\.\d+)(?:-(\d+)(?:-g[0-9a-fA-F]+)?)?", tag)
if not match:
    sys.exit(1)
base, count = match.groups()
print(base, int(count or 0))
PY
)" || continue

    distance="$(git -C "$REPO_ROOT" rev-list --count "${tag}..HEAD" 2>/dev/null || true)"
    [[ "$distance" =~ ^[0-9]+$ ]] || continue
    if [[ -z "$best_distance" || "$distance" -lt "$best_distance" ]]; then
      best_tag="$tag"
      best_distance="$distance"
      best_base="${parsed% *}"
      best_base_count="${parsed#* }"
    fi
  done < <(git -C "$REPO_ROOT" tag --merged HEAD --list 'v*' 2>/dev/null)

  short_hash="$(git -C "$REPO_ROOT" rev-parse --short HEAD)"
  dirty_suffix=""
  if is_dirty; then
    dirty_suffix="-dirty"
  fi

  if [[ -z "$best_tag" ]]; then
    printf 'v0.0.0-0-g%s%s' "$short_hash" "$dirty_suffix"
    return
  fi

  if [[ "$best_distance" == "0" && "$best_base_count" == "0" && -z "$dirty_suffix" ]]; then
    printf '%s' "$best_base"
    return
  fi

  base="$best_base"
  commit_count=$((best_base_count + best_distance))
  printf '%s-%s-g%s%s' "$base" "$commit_count" "$short_hash" "$dirty_suffix"
}

if [[ -n "${CLI_VERSION:-}" ]]; then
  trim "$CLI_VERSION"
else
  derive_from_git
fi

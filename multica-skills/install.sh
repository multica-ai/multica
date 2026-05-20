#!/usr/bin/env bash
# Upload one or more skills from this directory to a Multica workspace.
#
# Idempotent: if a skill with the same name already exists in the target
# workspace, its description/content is updated and supporting files are
# upserted in place. Otherwise a new skill is created.
#
# Requirements:
#   - `multica` CLI on PATH and authenticated (`multica login`)
#   - `MULTICA_WORKSPACE_ID` exported, OR pass --workspace-id <uuid>
#   - `jq`
#
# Usage:
#   bash multica-skills/install.sh Outline
#   bash multica-skills/install.sh OutlineLocal
#   bash multica-skills/install.sh Outline OutlineLocal
#   bash multica-skills/install.sh --workspace-id <uuid> Outline
#
# Each positional arg is a subdirectory name under multica-skills/.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

WS_ID=""
SKILLS=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --workspace-id) WS_ID="$2"; shift 2 ;;
    -h|--help)
      sed -n '1,21p' "$0" >&2
      exit 0
      ;;
    -*) echo "unknown flag: $1" >&2; exit 1 ;;
    *)  SKILLS+=("$1"); shift ;;
  esac
done

if [[ ${#SKILLS[@]} -eq 0 ]]; then
  echo "no skills specified — pass at least one subdirectory name" >&2
  echo "available:" >&2
  for d in "$ROOT"/*/; do
    [[ -f "$d/SKILL.md" ]] && echo "  $(basename "$d")" >&2
  done
  exit 1
fi

WS_ID="${WS_ID:-${MULTICA_WORKSPACE_ID:-}}"
if [[ -z "$WS_ID" ]]; then
  echo "workspace id required: pass --workspace-id <uuid> or export MULTICA_WORKSPACE_ID" >&2
  exit 1
fi

command -v multica >/dev/null 2>&1 || { echo "multica CLI not found on PATH" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }

echo "==> target workspace: $WS_ID"

install_one() {
  local name="$1"
  local dir="$ROOT/$name"

  if [[ ! -f "$dir/SKILL.md" ]]; then
    echo "  ! no SKILL.md at $dir — skipping" >&2
    return 1
  fi

  # Frontmatter parsing: `name:` and `description:` from the YAML block.
  local skill_name skill_desc skill_body
  skill_name=$(awk '
    /^---$/ { fm=!fm; next }
    fm && /^name:/ { sub(/^name:[[:space:]]*/, ""); print; exit }
  ' "$dir/SKILL.md")
  skill_desc=$(awk '
    /^---$/ { fm=!fm; next }
    fm && /^description:/ { sub(/^description:[[:space:]]*/, ""); print; exit }
  ' "$dir/SKILL.md")
  skill_body=$(awk '
    BEGIN { fm=0; done=0 }
    /^---$/ { if (!done) { fm=!fm; if (!fm) done=1; next } }
    { if (done) print }
  ' "$dir/SKILL.md")

  if [[ -z "$skill_name" ]]; then
    echo "  ! $name/SKILL.md missing 'name:' frontmatter — skipping" >&2
    return 1
  fi

  echo
  echo "==> $skill_name (from $name/)"

  local existing
  existing=$(MULTICA_WORKSPACE_ID="$WS_ID" multica skill list --output json \
             | jq -r --arg n "$skill_name" '.[]? | select(.name == $n) | .id' \
             | head -n1)

  local sid
  if [[ -n "$existing" ]]; then
    echo "  updating existing skill $existing"
    MULTICA_WORKSPACE_ID="$WS_ID" multica skill update "$existing" \
      --description "$skill_desc" \
      --content "$skill_body" \
      --output json >/dev/null
    sid="$existing"
  else
    echo "  creating new skill"
    sid=$(MULTICA_WORKSPACE_ID="$WS_ID" multica skill create \
          --name "$skill_name" \
          --description "$skill_desc" \
          --content "$skill_body" \
          --output json | jq -r .id)
    if [[ -z "$sid" || "$sid" == "null" ]]; then
      echo "  ! create did not return an id" >&2
      return 1
    fi
    echo "  created skill $sid"
  fi

  # Upsert every supporting file inside the skill directory (recursive),
  # preserving its relative path. Skip SKILL.md itself.
  local count=0
  while IFS= read -r -d '' f; do
    local rel="${f#"$dir"/}"
    [[ "$rel" == "SKILL.md" ]] && continue
    MULTICA_WORKSPACE_ID="$WS_ID" multica skill files upsert "$sid" \
      --path "$rel" \
      --content "$(cat "$f")" \
      --output json >/dev/null
    echo "    upsert $rel"
    count=$((count + 1))
  done < <(find "$dir" -type f -print0)

  echo "  done — $count supporting file(s)"
}

failures=0
for s in "${SKILLS[@]}"; do
  install_one "$s" || failures=$((failures + 1))
done

echo
if [[ $failures -gt 0 ]]; then
  echo "completed with $failures failure(s)" >&2
  exit 1
fi
echo "all skills installed."

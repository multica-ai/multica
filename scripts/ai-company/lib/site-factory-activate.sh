#!/usr/bin/env bash
# Site-factory activation: registry path + autopilot prerequisites for new repos.
# shellcheck shell=bash

# ACTIVATE_AUTOPILOT: -1 unset, 0 no, 1 yes

site_factory_resolve_autopilot_choice() {
  local create_repo="${1:-0}"

  if [ "$create_repo" -ne 1 ]; then
    ACTIVATE_AUTOPILOT=0
    return 0
  fi

  if [ "$ACTIVATE_AUTOPILOT" -eq -1 ]; then
    if [ -t 0 ]; then
      echo ""
      echo "新建 GitHub 仓库后，是否接入自迭代（registry + 本机路径 + 首票派单）？"
      read -r -p "接入自迭代? [Y/n] " ans || ans=""
      case "$ans" in
        [Nn]*) ACTIVATE_AUTOPILOT=0 ;;
        *) ACTIVATE_AUTOPILOT=1 ;;
      esac
    else
      ACTIVATE_AUTOPILOT=0
    fi
  fi

  if [ "$ACTIVATE_AUTOPILOT" -eq 1 ]; then
    if [ "$MAX_DISPATCH" -lt 1 ]; then
      MAX_DISPATCH=1
    fi
    if [ "${EXPLICIT_SKIP_REGISTRY:-0}" -eq 0 ]; then
      SKIP_REGISTRY=0
    fi
    if [ "${EXPLICIT_SKIP_DISPATCH:-0}" -eq 0 ]; then
      SKIP_DISPATCH=0
    fi
    echo "autopilot: enabled (registry + repo-path + dispatch)"
  else
    SKIP_REGISTRY=1
    SKIP_DISPATCH=1
    echo "autopilot: skipped (仅建站 + bootstrap Issues；稍后可 activate-project-autopilot.sh)"
  fi
}

site_factory_ensure_local_repo_path() {
  local slug="$1"
  local target="$2"
  local multica_root="$3"
  local script_dir="$4"
  local dry_run="${5:-0}"

  if [ "${ACTIVATE_AUTOPILOT:-0}" -ne 1 ]; then
    return 0
  fi

  if bash "$script_dir/resolve-repo-path.sh" --id "$slug" --quiet 2>/dev/null; then
    echo "repo-path: $slug already resolvable"
    return 0
  fi

  if [ "$dry_run" -eq 1 ]; then
    echo "[dry-run] repo-path: would register $slug -> $target"
    return 0
  fi

  local file="$multica_root/.ai-company/config/repo-paths.local.yaml"
  mkdir -p "$(dirname "$file")"
  touch "$file"

  if python3 - "$file" "$slug" "$target" <<'PY'
import sys
from pathlib import Path

path = Path(sys.argv[1])
slug, target = sys.argv[2], sys.argv[3]
lines = path.read_text(encoding="utf-8").splitlines() if path.is_file() else []
for line in lines:
    key = line.split(":", 1)[0].strip()
    if key == slug:
        sys.exit(0)
header = []
if not lines:
    header = [
        "# Auto-maintained by site-factory (gitignored). See repo-paths.local.yaml.example",
        "",
    ]
path.write_text(
    "\n".join([*header, *lines, f"{slug}: {target}", ""]).lstrip("\n"),
    encoding="utf-8",
)
PY
  then
    echo "repo-path: registered $slug -> $target in repo-paths.local.yaml"
  fi
}

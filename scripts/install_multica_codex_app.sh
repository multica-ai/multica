#!/usr/bin/env bash
set -Eeuo pipefail

PLUGIN_NAME="multica-codex-app"
MARKETPLACE_NAME="multica-local"
PLUGIN_SELECTOR="${PLUGIN_NAME}@${MARKETPLACE_NAME}"

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
MULTICA_HOME_DIR="${MULTICA_HOME:-${HOME}/.multica}"
TARGET_BIN_DIR="${MULTICA_BIN_DIR:-}"
TIMESTAMP="$(date -u '+%Y%m%d%H%M%S')"
USE_CODEX_HOME_ENV=0
DRY_RUN=0
SKIP_CLI_INSTALL=0

log() {
  printf '\n==> %s\n' "$*"
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

run() {
  printf '+'
  printf ' %q' "$@"
  printf '\n'
  if [[ "$DRY_RUN" == "0" ]]; then
    "$@"
  fi
}

detect_package_root() {
  if [[ -n "${MULTICA_CODEX_APP_PACKAGE_ROOT:-}" ]]; then
    printf '%s\n' "$MULTICA_CODEX_APP_PACKAGE_ROOT"
    return
  fi
  if [[ -d "${SCRIPT_DIR}/plugins/${PLUGIN_NAME}" ]]; then
    printf '%s\n' "$SCRIPT_DIR"
    return
  fi
  if [[ -d "${SCRIPT_DIR}/../plugins/${PLUGIN_NAME}" ]]; then
    (cd "${SCRIPT_DIR}/.." && pwd)
    return
  fi
  die "cannot locate plugins/${PLUGIN_NAME}; set MULTICA_CODEX_APP_PACKAGE_ROOT"
}

PACKAGE_ROOT="$(detect_package_root)"
MARKETPLACE_ROOT="${PACKAGE_ROOT}"
MARKETPLACE_FILE="${MARKETPLACE_ROOT}/.agents/plugins/marketplace.json"
PLUGIN_SOURCE_DIR="${MARKETPLACE_ROOT}/plugins/${PLUGIN_NAME}"
CODEX_HOME_DIR="${HOME}/.codex"
INSTALL_MARKETPLACE_ROOT=""
INSTALL_PLUGIN_DIR=""

usage() {
  cat <<USAGE
Usage: $(basename "$0") [--dry-run] [--codex-home-env] [--skip-cli-install]

Cleanly reinstall ${PLUGIN_SELECTOR}.

Expected package layout:

  .agents/plugins/marketplace.json
  cli/multica                         optional when --skip-cli-install is used
  plugins/${PLUGIN_NAME}/

By default, the script unsets CODEX_HOME for codex plugin commands so it targets
the normal Codex App environment under ${HOME}/.codex. Use --codex-home-env only
when you intentionally want to operate on the current CODEX_HOME.

Environment overrides:
  MULTICA_CODEX_APP_PACKAGE_ROOT  Package or repository root.
  MULTICA_HOME                    Multica home directory. Default: ${HOME}/.multica
  MULTICA_BIN_DIR                 Target directory for the running multica CLI.
  MULTICA_SOURCE_BIN              Source multica binary to install.
USAGE
}

for arg in "$@"; do
  case "$arg" in
    --dry-run)
      DRY_RUN=1
      ;;
    --codex-home-env)
      USE_CODEX_HOME_ENV=1
      CODEX_HOME_DIR="${CODEX_HOME:-${HOME}/.codex}"
      ;;
    --skip-cli-install)
      SKIP_CLI_INSTALL=1
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $arg" >&2
      usage >&2
      exit 2
      ;;
  esac
done

codex_cmd() {
  if [[ "$USE_CODEX_HOME_ENV" == "1" ]]; then
    run codex "$@"
  else
    run env -u CODEX_HOME codex "$@"
  fi
}

codex_capture() {
  if [[ "$USE_CODEX_HOME_ENV" == "1" ]]; then
    codex "$@"
  else
    env -u CODEX_HOME codex "$@"
  fi
}

codex_plugin_add_subcommand() {
  local plugin_help
  plugin_help="$(codex_capture plugin --help 2>&1 || true)"
  if printf '%s\n' "$plugin_help" | awk '$1 == "add" { found = 1 } END { exit found ? 0 : 1 }'; then
    printf 'add\n'
    return
  fi
  if printf '%s\n' "$plugin_help" | awk '$1 == "a" { found = 1 } END { exit found ? 0 : 1 }'; then
    printf 'a\n'
    return
  fi
  die "codex plugin install subcommand not found; expected 'add' or 'a'"
}

remove_path() {
  local path="$1"
  if [[ -e "$path" || -L "$path" ]]; then
    run rm -rf "$path"
  else
    printf 'skip missing path: %s\n' "$path"
  fi
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

resolve_target_bin_dir() {
  if [[ -n "$TARGET_BIN_DIR" ]]; then
    printf '%s\n' "$TARGET_BIN_DIR"
    return
  fi
  if command -v multica >/dev/null 2>&1; then
    dirname -- "$(command -v multica)"
    return
  fi
  printf '%s\n' "${MULTICA_HOME_DIR}/bin"
}

resolve_source_multica_bin() {
  if [[ -n "${MULTICA_SOURCE_BIN:-}" ]]; then
    printf '%s\n' "$MULTICA_SOURCE_BIN"
    return
  fi
  if [[ -f "${PACKAGE_ROOT}/cli/multica" ]]; then
    printf '%s\n' "${PACKAGE_ROOT}/cli/multica"
    return
  fi
  if [[ -f "${PACKAGE_ROOT}/server/bin/multica" ]]; then
    printf '%s\n' "${PACKAGE_ROOT}/server/bin/multica"
    return
  fi
  if [[ -f "${PACKAGE_ROOT}/server/multica" ]]; then
    printf '%s\n' "${PACKAGE_ROOT}/server/multica"
    return
  fi
  die "missing source multica binary; provide cli/multica, run make build, set MULTICA_SOURCE_BIN, or use --skip-cli-install"
}

daemon_is_running() {
  local multica_bin="$1"
  [[ -x "$multica_bin" ]] || return 1
  "$multica_bin" daemon status 2>&1 | grep -Eiq '^Daemon:[[:space:]]+running\b'
}

package_root_is_git_worktree() {
  git -C "$PACKAGE_ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1
}

prepare_install_source() {
  [[ -d "$PLUGIN_SOURCE_DIR" ]] || die "missing plugin directory: $PLUGIN_SOURCE_DIR"
  [[ -f "$MARKETPLACE_FILE" ]] || die "missing marketplace file: $MARKETPLACE_FILE"
  [[ -f "${PLUGIN_SOURCE_DIR}/.mcp.json" ]] || die "missing MCP config: ${PLUGIN_SOURCE_DIR}/.mcp.json"
  [[ -f "${PLUGIN_SOURCE_DIR}/hooks/hooks.json" ]] || die "missing hooks config: ${PLUGIN_SOURCE_DIR}/hooks/hooks.json"

  if package_root_is_git_worktree && [[ "${MULTICA_CODEX_APP_USE_SOURCE_ROOT:-0}" != "1" ]]; then
    local stage_root="${MULTICA_HOME_DIR}/codex-app-plugin-install/${PLUGIN_NAME}-${TIMESTAMP}"
    log "Stage Codex plugin package"
    run mkdir -p "${stage_root}/plugins" "${stage_root}/.agents/plugins"
    run cp -pR "$PLUGIN_SOURCE_DIR" "${stage_root}/plugins/"
    run cp -pR "$MARKETPLACE_FILE" "${stage_root}/.agents/plugins/"
    INSTALL_MARKETPLACE_ROOT="$stage_root"
    INSTALL_PLUGIN_DIR="${stage_root}/plugins/${PLUGIN_NAME}"
    return
  fi

  INSTALL_MARKETPLACE_ROOT="$MARKETPLACE_ROOT"
  INSTALL_PLUGIN_DIR="$PLUGIN_SOURCE_DIR"
}

ensure_marketplace() {
  log "Ensure Codex marketplace ${MARKETPLACE_NAME}"
  local marketplace_line
  marketplace_line="$(codex_capture plugin marketplace list 2>/dev/null | awk -v name="$MARKETPLACE_NAME" '$1 == name {print; exit}' || true)"

  if [[ -n "$marketplace_line" ]]; then
    local configured_root
    configured_root="$(printf '%s\n' "$marketplace_line" | awk '{print $2}')"
    if [[ "$configured_root" == "$INSTALL_MARKETPLACE_ROOT" ]]; then
      printf 'marketplace already configured: %s -> %s\n' "$MARKETPLACE_NAME" "$INSTALL_MARKETPLACE_ROOT"
      return
    fi
    printf 'marketplace %s points to %s, replacing with %s\n' "$MARKETPLACE_NAME" "$configured_root" "$INSTALL_MARKETPLACE_ROOT"
    codex_cmd plugin marketplace remove "$MARKETPLACE_NAME"
  fi

  codex_cmd plugin marketplace add "$INSTALL_MARKETPLACE_ROOT"
}

remove_plugin_installation() {
  log "Remove previous Codex plugin installation"
  if codex_capture plugin list 2>/dev/null | grep -q "${PLUGIN_SELECTOR}"; then
    codex_cmd plugin remove "$PLUGIN_SELECTOR"
  else
    printf 'plugin is not currently listed as installed: %s\n' "$PLUGIN_SELECTOR"
  fi

  log "Remove Codex plugin cache"
  remove_path "${HOME}/.codex/plugins/cache/${MARKETPLACE_NAME}/${PLUGIN_NAME}"
  if [[ "$USE_CODEX_HOME_ENV" == "1" && "$CODEX_HOME_DIR" != "${HOME}/.codex" ]]; then
    remove_path "${CODEX_HOME_DIR}/plugins/cache/${MARKETPLACE_NAME}/${PLUGIN_NAME}"
  elif [[ -n "${CODEX_HOME:-}" && "$CODEX_HOME" != "${HOME}/.codex" ]]; then
    remove_path "${CODEX_HOME}/plugins/cache/${MARKETPLACE_NAME}/${PLUGIN_NAME}"
  fi
}

replace_multica_cli() {
  local source_multica="$1"
  local target_bin_dir="$2"
  local target_multica="${target_bin_dir}/multica"

  [[ -f "$source_multica" ]] || die "missing source multica binary: $source_multica"

  log "Stop Multica daemon if it is running"
  if daemon_is_running "$target_multica"; then
    run "$target_multica" daemon stop
  else
    printf 'daemon is not running or current CLI is not executable: %s\n' "$target_multica"
  fi

  log "Back up current Multica CLI"
  local backup_dir="${MULTICA_HOME_DIR}/backups/multica-codex-app-install-${TIMESTAMP}"
  run mkdir -p "$backup_dir"
  run mkdir -p "$target_bin_dir"

  if [[ -e "$target_multica" || -L "$target_multica" ]]; then
    run cp -pR "$target_multica" "$backup_dir/"
  else
    printf 'no existing target to back up: %s\n' "$target_multica"
  fi

  log "Copy new Multica CLI"
  run cp -p "$source_multica" "$target_multica"
  if [[ "$DRY_RUN" == "0" && -e "$target_multica" ]]; then
    run chmod 0755 "$target_multica"
    xattr -d com.apple.quarantine "$target_multica" >/dev/null 2>&1 || true
  elif [[ "$DRY_RUN" == "1" ]]; then
    run chmod 0755 "$target_multica"
  fi

  log "Start Multica daemon"
  run "$target_multica" daemon start
  run "$target_multica" daemon status

  printf '\nbackup directory: %s\n' "$backup_dir"
}

rewrite_plugin_cli_paths() {
  local multica_bin="$1"
  local mcp_config="${INSTALL_PLUGIN_DIR}/.mcp.json"
  local hooks_config="${INSTALL_PLUGIN_DIR}/hooks/hooks.json"

  [[ -x "$multica_bin" ]] || die "multica binary is not executable: $multica_bin"

  log "Rewrite Codex plugin CLI paths"
  if [[ "$DRY_RUN" == "1" ]]; then
    printf '+ rewrite %s and %s to use %s\n' "$mcp_config" "$hooks_config" "$multica_bin"
    printf 'plugin CLI path: %s\n' "$multica_bin"
    return
  fi
  [[ -f "$mcp_config" ]] || die "missing MCP config: $mcp_config"
  [[ -f "$hooks_config" ]] || die "missing hooks config: $hooks_config"
  PLUGIN_MCP_CONFIG="$mcp_config" \
  PLUGIN_HOOKS_CONFIG="$hooks_config" \
  PLUGIN_MULTICA_BIN="$multica_bin" \
  python3 - <<'PY'
import json
import os
from pathlib import Path

mcp_path = Path(os.environ["PLUGIN_MCP_CONFIG"])
hooks_path = Path(os.environ["PLUGIN_HOOKS_CONFIG"])
multica_bin = os.environ["PLUGIN_MULTICA_BIN"]

with mcp_path.open("r", encoding="utf-8") as fh:
    mcp = json.load(fh)
mcp.setdefault("mcpServers", {}).setdefault("multica", {})["command"] = multica_bin
with mcp_path.open("w", encoding="utf-8") as fh:
    json.dump(mcp, fh, indent=2)
    fh.write("\n")

with hooks_path.open("r", encoding="utf-8") as fh:
    hooks = json.load(fh)
for entries in hooks.get("hooks", {}).values():
    if not isinstance(entries, list):
        continue
    for entry in entries:
        for hook in entry.get("hooks", []):
            if hook.get("type") == "command":
                hook["command"] = f"{multica_bin} codex-plugin hook"
with hooks_path.open("w", encoding="utf-8") as fh:
    json.dump(hooks, fh, indent=2)
    fh.write("\n")
PY

  printf 'plugin CLI path: %s\n' "$multica_bin"
}

install_plugin() {
  log "Install Codex plugin"
  ensure_marketplace
  local add_subcommand
  add_subcommand="$(codex_plugin_add_subcommand)"
  codex_cmd plugin "$add_subcommand" "$PLUGIN_SELECTOR"
  codex_cmd plugin list
}

main() {
  need_cmd codex
  need_cmd awk
  need_cmd grep
  need_cmd cp
  need_cmd rm
  need_cmd python3

  local target_bin_dir source_multica
  target_bin_dir="$(resolve_target_bin_dir)"

  if [[ "$SKIP_CLI_INSTALL" == "0" ]]; then
    source_multica="$(resolve_source_multica_bin)"
  else
    source_multica=""
  fi

  log "Install context"
  printf 'package root:    %s\n' "$PACKAGE_ROOT"
  printf 'plugin source:   %s\n' "$PLUGIN_SOURCE_DIR"
  printf 'marketplace:     %s\n' "$MARKETPLACE_ROOT"
  printf 'codex home:      %s\n' "$CODEX_HOME_DIR"
  printf 'codex env mode:  %s\n' "$([[ "$USE_CODEX_HOME_ENV" == "1" ]] && printf 'current CODEX_HOME' || printf 'default ~/.codex')"
  printf 'multica home:    %s\n' "$MULTICA_HOME_DIR"
  printf 'target bin dir:  %s\n' "$target_bin_dir"
  printf 'source CLI:      %s\n' "$([[ -n "$source_multica" ]] && printf '%s' "$source_multica" || printf '<skipped>')"
  printf 'plugin selector: %s\n' "$PLUGIN_SELECTOR"

  prepare_install_source
  printf 'install root:    %s\n' "$INSTALL_MARKETPLACE_ROOT"

  remove_plugin_installation
  if [[ "$SKIP_CLI_INSTALL" == "0" ]]; then
    replace_multica_cli "$source_multica" "$target_bin_dir"
  else
    log "Skip Multica CLI install"
    [[ -x "${target_bin_dir}/multica" ]] || die "installed multica binary is not executable: ${target_bin_dir}/multica"
  fi
  rewrite_plugin_cli_paths "${target_bin_dir}/multica"
  install_plugin

  log "Done"
  printf 'Reopen Codex App and allow plugin hooks again if prompted.\n'
}

main "$@"

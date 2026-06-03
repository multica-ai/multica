#!/usr/bin/env bash
set -Eeuo pipefail

PLUGIN_NAME="multica-codex-app"
MARKETPLACE_NAME="multica-local"

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
PLUGIN_SOURCE_DIR="${REPO_ROOT}/plugins/${PLUGIN_NAME}"
MARKETPLACE_FILE="${REPO_ROOT}/.agents/plugins/marketplace.json"
INSTALL_SCRIPT="${REPO_ROOT}/scripts/install_multica_codex_app.sh"
CLI_ARTIFACTS_SOURCE_DIR="${CLI_ARTIFACTS_DIR:-${REPO_ROOT}/artifacts/cli}"

usage() {
  cat <<USAGE
Usage: $(basename "$0") <output-dir> [--force]

Package the Multica Codex App plugin into the same distribution layout used for
manual user installs:

  <output-dir>/
    .agents/plugins/marketplace.json
    cli/multica
    cli-artifacts/                  full-platform CLI release artifacts
    install_multica_codex_app.sh
    INSTALL_WITH_CODEX_APP.md
    plugins/${PLUGIN_NAME}/
    ${PLUGIN_NAME}-<version>/
    ${PLUGIN_NAME}-<version>.tar.gz
    ${PLUGIN_NAME}-<version>.zip

Environment overrides:
  MULTICA_SOURCE_BIN       Optional source binary for cli/multica. When unset,
                           the current-platform CLI is built automatically.
  CLI_ARTIFACTS_DIR        Output/source directory for full-platform CLI artifacts.
  MULTICA_SKIP_CLI_BUILD   Set to 1 to skip automatic CLI builds.
  MULTICA_PLUGIN_VERSION   Package/cachebuster version override.
USAGE
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

log() {
  printf '\n==> %s\n' "$*"
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

abs_path() {
  local path="$1"
  mkdir -p "$(dirname -- "$path")"
  if [[ -d "$path" ]]; then
    (cd "$path" && pwd)
  else
    local dir base
    dir="$(cd "$(dirname -- "$path")" && pwd)"
    base="$(basename -- "$path")"
    printf '%s/%s\n' "$dir" "$base"
  fi
}

sha_for_cachebuster() {
  git -C "$REPO_ROOT" rev-parse --short=9 HEAD 2>/dev/null || printf 'unknown'
}

derive_plugin_version() {
  if [[ -n "${MULTICA_PLUGIN_VERSION:-}" ]]; then
    printf '%s\n' "$MULTICA_PLUGIN_VERSION"
    return
  fi

  python3 - <<'PY' "$PLUGIN_SOURCE_DIR/.codex-plugin/plugin.json" "$(date -u '+%Y%m%d%H%M%S')" "$(sha_for_cachebuster)"
import json
import re
import sys
from pathlib import Path

plugin_json = Path(sys.argv[1])
stamp = sys.argv[2]
commit = sys.argv[3]
data = json.loads(plugin_json.read_text(encoding="utf-8"))
current = data.get("version", "0.1.0")
base = current.split("+", 1)[0]
if not re.fullmatch(r"[0-9A-Za-z][0-9A-Za-z._-]*", base):
    raise SystemExit(f"invalid plugin version base: {base}")
print(f"{base}+codex.{stamp}-{commit}")
PY
}

resolve_source_multica_bin() {
  if [[ -n "${MULTICA_SOURCE_BIN:-}" ]]; then
    printf '%s\n' "$MULTICA_SOURCE_BIN"
    return
  fi
  if [[ -f "${REPO_ROOT}/server/bin/multica" ]]; then
    printf '%s\n' "${REPO_ROOT}/server/bin/multica"
    return
  fi
  if [[ -f "${REPO_ROOT}/server/multica" ]]; then
    printf '%s\n' "${REPO_ROOT}/server/multica"
    return
  fi
  if command -v multica >/dev/null 2>&1; then
    command -v multica
    return
  fi
  die "missing multica binary; run make build or set MULTICA_SOURCE_BIN"
}

build_current_platform_cli() {
  if [[ -n "${MULTICA_SOURCE_BIN:-}" ]]; then
    printf 'skip current-platform CLI build; MULTICA_SOURCE_BIN is set\n'
    return
  fi
  log "Build current-platform Multica CLI"
  (cd "$REPO_ROOT" && make build-cli)
}

build_full_platform_cli_artifacts() {
  log "Build full-platform Multica CLI artifacts"
  CLI_ARTIFACTS_DIR="$CLI_ARTIFACTS_SOURCE_DIR" bash "${REPO_ROOT}/scripts/build-cli-artifacts.sh"
}

file_size() {
  local file="$1"
  if stat -c '%s' "$file" >/dev/null 2>&1; then
    stat -c '%s' "$file"
  else
    stat -f '%z' "$file"
  fi
}

url_encode_path() {
  python3 - <<'PY' "$1"
import sys
from urllib.parse import quote

print(quote(sys.argv[1], safe=""))
PY
}

write_install_doc() {
  local package_root="$1"
  local doc_path="$2"
  local marketplace_path="${package_root}/.agents/plugins/marketplace.json"
  local encoded_marketplace
  encoded_marketplace="$(url_encode_path "$marketplace_path")"

  cat > "$doc_path" <<EOF
# Multica Codex App Plugin

## 一键安装

先退出 Codex App，然后执行：

\`\`\`bash
cd "${package_root}"
chmod +x install_multica_codex_app.sh
./install_multica_codex_app.sh
\`\`\`

如需先检查流程：

\`\`\`bash
./install_multica_codex_app.sh --dry-run
\`\`\`

脚本会清理旧 plugin/cache、停止并重启 \`multica daemon\`、备份并替换本机 Multica CLI，然后重新安装：

\`\`\`text
${PLUGIN_NAME}@${MARKETPLACE_NAME}
\`\`\`

## Codex Desktop App 安装入口

[View ${PLUGIN_NAME}](codex://plugins/${PLUGIN_NAME}?marketplacePath=${encoded_marketplace})

[Share ${PLUGIN_NAME}](codex://plugins/${PLUGIN_NAME}?marketplacePath=${encoded_marketplace}&mode=share)

## 当前产物

\`\`\`text
Plugin: ${package_root}/plugins/${PLUGIN_NAME}
Marketplace: ${marketplace_path}
CLI: ${package_root}/cli/multica
Install script: ${package_root}/install_multica_codex_app.sh
\`\`\`
EOF
}

rewrite_packaged_plugin_version() {
  local plugin_json="$1"
  local version="$2"
  python3 - <<'PY' "$plugin_json" "$version"
import json
import sys
from pathlib import Path

path = Path(sys.argv[1])
version = sys.argv[2]
data = json.loads(path.read_text(encoding="utf-8"))
data["version"] = version
path.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")
PY
}

validate_packaged_json() {
  local package_root="$1"
  python3 -m json.tool "${package_root}/.agents/plugins/marketplace.json" >/dev/null
  python3 -m json.tool "${package_root}/plugins/${PLUGIN_NAME}/.codex-plugin/plugin.json" >/dev/null
  python3 -m json.tool "${package_root}/plugins/${PLUGIN_NAME}/.mcp.json" >/dev/null
  python3 -m json.tool "${package_root}/plugins/${PLUGIN_NAME}/hooks/hooks.json" >/dev/null
}

build_archives() {
  local output_dir="$1"
  local package_name="$2"
  local stage_dir="${output_dir}/${package_name}"
  local tar_path="${output_dir}/${package_name}.tar.gz"
  local zip_path="${output_dir}/${package_name}.zip"

  rm -rf "$stage_dir" "$tar_path" "$zip_path"
  mkdir -p "$stage_dir"
  cp -pR "${output_dir}/.agents" "$stage_dir/"
  cp -pR "${output_dir}/cli" "$stage_dir/"
  if [[ -d "${output_dir}/cli-artifacts" ]]; then
    cp -pR "${output_dir}/cli-artifacts" "$stage_dir/"
  fi
  cp -pR "${output_dir}/plugins" "$stage_dir/"
  cp -p "${output_dir}/install_multica_codex_app.sh" "$stage_dir/"
  cp -p "${output_dir}/INSTALL_WITH_CODEX_APP.md" "$stage_dir/"

  (
    cd "$output_dir"
    tar -czf "$tar_path" "$package_name"
    zip -qr "$zip_path" "$package_name"
  )
}

OUTPUT_DIR=""
FORCE=0
for arg in "$@"; do
  case "$arg" in
    --force)
      FORCE=1
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    -*)
      echo "Unknown argument: $arg" >&2
      usage >&2
      exit 2
      ;;
    *)
      if [[ -n "$OUTPUT_DIR" ]]; then
        die "only one output directory may be provided"
      fi
      OUTPUT_DIR="$arg"
      ;;
  esac
done

[[ -n "$OUTPUT_DIR" ]] || {
  usage >&2
  exit 2
}

need_cmd cp
need_cmd rm
need_cmd tar
need_cmd zip
need_cmd python3

[[ -d "$PLUGIN_SOURCE_DIR" ]] || die "missing plugin source directory: $PLUGIN_SOURCE_DIR"
[[ -f "$MARKETPLACE_FILE" ]] || die "missing marketplace file: $MARKETPLACE_FILE"
[[ -f "$INSTALL_SCRIPT" ]] || die "missing install script: $INSTALL_SCRIPT"

if [[ "${MULTICA_SKIP_CLI_BUILD:-0}" != "1" ]]; then
  need_cmd go
  build_current_platform_cli
  build_full_platform_cli_artifacts
else
  printf 'skip automatic CLI builds; MULTICA_SKIP_CLI_BUILD=1\n'
fi

SOURCE_MULTICA="$(resolve_source_multica_bin)"
[[ -f "$SOURCE_MULTICA" ]] || die "missing source multica binary: $SOURCE_MULTICA"

OUTPUT_DIR="$(abs_path "$OUTPUT_DIR")"
PLUGIN_VERSION="$(derive_plugin_version)"
PACKAGE_NAME="${PLUGIN_NAME}-${PLUGIN_VERSION}"

if [[ -e "$OUTPUT_DIR" && "$FORCE" != "1" ]]; then
  if [[ "$(find "$OUTPUT_DIR" -mindepth 1 -maxdepth 1 -print -quit 2>/dev/null)" ]]; then
    die "output directory is not empty: $OUTPUT_DIR; pass --force to replace it"
  fi
fi

log "Package context"
printf 'repo root:       %s\n' "$REPO_ROOT"
printf 'output dir:      %s\n' "$OUTPUT_DIR"
printf 'plugin version:  %s\n' "$PLUGIN_VERSION"
printf 'package name:    %s\n' "$PACKAGE_NAME"
printf 'source CLI:      %s\n' "$SOURCE_MULTICA"
printf 'CLI artifacts:   %s\n' "$CLI_ARTIFACTS_SOURCE_DIR"

log "Create package layout"
rm -rf "$OUTPUT_DIR"
mkdir -p "${OUTPUT_DIR}/cli" "${OUTPUT_DIR}/plugins" "${OUTPUT_DIR}/.agents/plugins"
cp -pR "$PLUGIN_SOURCE_DIR" "${OUTPUT_DIR}/plugins/"
cp -p "$MARKETPLACE_FILE" "${OUTPUT_DIR}/.agents/plugins/marketplace.json"
cp -p "$INSTALL_SCRIPT" "${OUTPUT_DIR}/install_multica_codex_app.sh"
cp -p "$SOURCE_MULTICA" "${OUTPUT_DIR}/cli/multica"
chmod 0755 "${OUTPUT_DIR}/install_multica_codex_app.sh" "${OUTPUT_DIR}/cli/multica"

[[ -d "$CLI_ARTIFACTS_SOURCE_DIR" ]] || die "missing CLI artifacts directory after build: $CLI_ARTIFACTS_SOURCE_DIR"
cp -pR "$CLI_ARTIFACTS_SOURCE_DIR" "${OUTPUT_DIR}/cli-artifacts"

rewrite_packaged_plugin_version "${OUTPUT_DIR}/plugins/${PLUGIN_NAME}/.codex-plugin/plugin.json" "$PLUGIN_VERSION"
write_install_doc "$OUTPUT_DIR" "${OUTPUT_DIR}/INSTALL_WITH_CODEX_APP.md"
validate_packaged_json "$OUTPUT_DIR"

log "Build archives"
build_archives "$OUTPUT_DIR" "$PACKAGE_NAME"

log "Done"
printf 'package root: %s\n' "$OUTPUT_DIR"
printf 'archive tar:  %s/%s.tar.gz (%s bytes)\n' "$OUTPUT_DIR" "$PACKAGE_NAME" "$(file_size "${OUTPUT_DIR}/${PACKAGE_NAME}.tar.gz")"
printf 'archive zip:  %s/%s.zip (%s bytes)\n' "$OUTPUT_DIR" "$PACKAGE_NAME" "$(file_size "${OUTPUT_DIR}/${PACKAGE_NAME}.zip")"

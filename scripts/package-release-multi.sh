#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_DIR="$ROOT_DIR/server"
SERVER_BIN_ROOT="${RELEASE_BIN_ROOT:-$SERVER_DIR/bin/release}"
WORKSPACE_DIST_DIR="$ROOT_DIR/apps/workspace/dist"
MIGRATIONS_DIR="$SERVER_DIR/migrations"
ENV_TEMPLATE="$ROOT_DIR/deploy/server.env.example"

TARGETS=()

log_step() {
  printf '\n==> %s\n' "$1"
}

require_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "缺少依赖命令: $cmd" >&2
    exit 1
  fi
}

require_file() {
  local path="$1"
  if [ ! -f "$path" ]; then
    echo "缺少文件: $path" >&2
    exit 1
  fi
}

require_dir() {
  local path="$1"
  if [ ! -d "$path" ]; then
    echo "缺少目录: $path" >&2
    exit 1
  fi
}

resolve_release_root() {
  local raw="${1:-}"
  if [ -z "$raw" ]; then
    echo "$ROOT_DIR/dist/release-multi"
    return
  fi
  if [[ "$raw" = /* ]]; then
    echo "$raw"
    return
  fi
  echo "$ROOT_DIR/$raw"
}

validate_target() {
  case "$1" in
    linux/amd64|darwin/arm64)
      ;;
    *)
      echo "不支持的目标平台: $1" >&2
      echo "当前仅支持: linux/amd64, darwin/arm64" >&2
      exit 1
      ;;
  esac
}

parse_targets() {
  local raw_targets
  local target
  local existing

  if [ "$#" -gt 0 ]; then
    raw_targets="$*"
  else
    raw_targets="${RELEASE_TARGETS:-linux/amd64,darwin/arm64}"
  fi

  raw_targets="${raw_targets//,/ }"

  for target in $raw_targets; do
    validate_target "$target"

    for existing in "${TARGETS[@]:-}"; do
      if [ "$existing" = "$target" ]; then
        continue 2
      fi
    done

    TARGETS+=("$target")
  done

  if [ "${#TARGETS[@]}" -eq 0 ]; then
    echo "至少需要一个目标平台" >&2
    exit 1
  fi
}

target_dir_name() {
  echo "${1/\//-}"
}

release_name() {
  echo "multica-backend-$(target_dir_name "$1")"
}

staged_target_dir() {
  echo "$SERVER_BIN_ROOT/$(target_dir_name "$1")"
}

build_if_needed() {
  if [ "${SKIP_BUILD:-0}" = "1" ]; then
    return
  fi

  "$ROOT_DIR/scripts/build-release-multi.sh" "${TARGETS[@]}"
}

validate_inputs() {
  local target
  local target_dir

  require_dir "$WORKSPACE_DIST_DIR"
  require_dir "$MIGRATIONS_DIR"
  require_file "$ENV_TEMPLATE"

  for target in "${TARGETS[@]}"; do
    target_dir="$(staged_target_dir "$target")"
    require_file "$target_dir/server"
    require_file "$target_dir/migrate"
  done
}

prepare_release_root() {
  local target
  mkdir -p "$RELEASE_ROOT"
  rm -rf "$RELEASE_ROOT/workspace"

  for target in "${TARGETS[@]}"; do
    rm -rf "$RELEASE_ROOT/$(release_name "$target")"
    rm -f "$RELEASE_ROOT/$(release_name "$target").tar.gz"
  done
}

copy_workspace() {
  log_step "复制共享 workspace 产物"
  mkdir -p "$RELEASE_ROOT/workspace"
  cp -R "$WORKSPACE_DIST_DIR"/. "$RELEASE_ROOT/workspace/"
}

package_target() {
  local target="$1"
  local target_dir="$(staged_target_dir "$target")"
  local name="$(release_name "$target")"
  local release_dir="$RELEASE_ROOT/$name"
  local archive_path="$RELEASE_ROOT/$name.tar.gz"

  log_step "组装后端发布包 ($target)"

  mkdir -p "$release_dir/migrations" "$release_dir/config"
  cp "$target_dir/server" "$release_dir/server"
  cp "$target_dir/migrate" "$release_dir/migrate"
  cp -R "$MIGRATIONS_DIR"/. "$release_dir/migrations/"
  cp "$ENV_TEMPLATE" "$release_dir/config/server.env.example"

  log_step "创建压缩包 ($target)"
  tar -C "$RELEASE_ROOT" -czf "$archive_path" "$name"
}

print_release_layout() {
  log_step "发布目录结构"
  (
    cd "$RELEASE_ROOT"
    find . -maxdepth 2 | LC_ALL=C sort
  )
}

print_summary() {
  local target
  log_step "多平台发布目录已生成"
  echo "workspace 目录: $RELEASE_ROOT/workspace"
  for target in "${TARGETS[@]}"; do
    echo "$target 目录: $RELEASE_ROOT/$(release_name "$target")"
    echo "$target 压缩包: $RELEASE_ROOT/$(release_name "$target").tar.gz"
  done
}

main() {
  local output_dir="${1:-}"
  local target

  require_cmd tar

  parse_targets "${@:2}"
  RELEASE_ROOT="$(resolve_release_root "$output_dir")"

  build_if_needed
  validate_inputs
  prepare_release_root
  copy_workspace

  for target in "${TARGETS[@]}"; do
    package_target "$target"
  done

  print_release_layout
  print_summary
}

main "$@"
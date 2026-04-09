#!/usr/bin/env bash
set -euo pipefail

# 获取仓库根目录，确保发布目录总是基于仓库路径生成。
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKSPACE_DIST_DIR="$ROOT_DIR/apps/workspace/dist"
SERVER_BIN="$ROOT_DIR/server/bin/server"
MIGRATE_BIN="$ROOT_DIR/server/bin/migrate"
MIGRATIONS_DIR="$ROOT_DIR/server/migrations"
ENV_TEMPLATE="$ROOT_DIR/deploy/server.env.example"

# 解析发布输出目录，未传入时默认写到 dist/release。
resolve_release_dir() {
  local raw="${1:-}"
  if [ -z "$raw" ]; then
    echo "$ROOT_DIR/dist/release"
    return
  fi
  if [[ "$raw" = /* ]]; then
    echo "$raw"
    return
  fi
  echo "$ROOT_DIR/$raw"
}

RELEASE_DIR="$(resolve_release_dir "${1:-}")"

# 输出统一的步骤日志，方便定位组装过程。
log_step() {
  printf '\n==> %s\n' "$1"
}

# 校验发布所需的文件是否已准备好，避免生成残缺目录。
require_file() {
  local path="$1"
  if [ ! -f "$path" ]; then
    echo "缺少文件: $path" >&2
    exit 1
  fi
}

# 校验发布所需的目录是否已准备好，避免生成残缺目录。
require_dir() {
  local path="$1"
  if [ ! -d "$path" ]; then
    echo "缺少目录: $path" >&2
    exit 1
  fi
}

# 默认先执行构建，除非显式要求跳过。
build_if_needed() {
  if [ "${SKIP_BUILD:-0}" = "1" ]; then
    return
  fi
  "$ROOT_DIR/scripts/build-release.sh"
}

# 组装可直接上传到 VPS 的发布目录。
assemble_release() {
  log_step "组装发布目录"

  rm -rf "$RELEASE_DIR"
  mkdir -p "$RELEASE_DIR/workspace" "$RELEASE_DIR/migrations" "$RELEASE_DIR/config"

  cp "$SERVER_BIN" "$RELEASE_DIR/server"
  cp "$MIGRATE_BIN" "$RELEASE_DIR/migrate"
  cp -R "$MIGRATIONS_DIR"/. "$RELEASE_DIR/migrations/"
  cp -R "$WORKSPACE_DIST_DIR"/. "$RELEASE_DIR/workspace/"
  cp "$ENV_TEMPLATE" "$RELEASE_DIR/config/server.env.example"
}

# 打印最终发布目录结构，方便确认上传内容。
print_release_layout() {
  log_step "发布目录结构"
  (
    cd "$RELEASE_DIR"
    find . -maxdepth 2 | LC_ALL=C sort
  )
}

# 汇总发布流程，确保产物完整后再输出目录结构。
main() {
  build_if_needed

  require_file "$SERVER_BIN"
  require_file "$MIGRATE_BIN"
  require_dir "$MIGRATIONS_DIR"
  require_dir "$WORKSPACE_DIST_DIR"
  require_file "$ENV_TEMPLATE"

  assemble_release
  print_release_layout

  log_step "发布目录已生成"
  echo "$RELEASE_DIR"
}

main "$@"

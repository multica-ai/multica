#!/usr/bin/env bash
set -euo pipefail

# 获取仓库根目录，确保脚本可以从任意工作目录执行。
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_DIR="$ROOT_DIR/server"

# 输出统一的步骤日志，方便排查构建阶段。
log_step() {
  printf '\n==> %s\n' "$1"
}

# 检查关键命令是否存在，避免构建跑到一半才失败。
require_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "缺少依赖命令: $cmd" >&2
    exit 1
  fi
}

# 构建 workspace 静态资源，并强制使用同域部署的默认配置。
build_workspace() {
  log_step "构建 workspace 静态产物"
  (
    cd "$ROOT_DIR"
    VITE_API_URL="" VITE_WS_URL="" pnpm --filter @multica/workspace build
  )
}

# 构建部署所需的后端二进制，只包含 server 和 migrate。
build_server_binaries() {
  log_step "构建后端二进制"
  (
    cd "$SERVER_DIR"
    go build -o bin/server ./cmd/server
    go build -o bin/migrate ./cmd/migrate
  )
}

# 汇总构建入口，统一管理依赖检查和产物输出。
main() {
  require_cmd pnpm
  require_cmd go

  build_workspace
  build_server_binaries

  log_step "构建完成"
  echo "workspace 产物: $ROOT_DIR/apps/workspace/dist"
  echo "server 二进制: $SERVER_DIR/bin/server"
  echo "migrate 二进制: $SERVER_DIR/bin/migrate"
}

main "$@"

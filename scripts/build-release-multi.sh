#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_DIR="$ROOT_DIR/server"
SERVER_BIN_ROOT="${RELEASE_BIN_ROOT:-$SERVER_DIR/bin/release}"

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

build_workspace() {
  log_step "构建 workspace 静态产物"
  (
    cd "$ROOT_DIR"
    VITE_API_URL="" VITE_WS_URL="" pnpm --filter @multica/workspace build
  )
}

build_target_binaries() {
  local target="$1"
  local goos="${target%/*}"
  local goarch="${target#*/}"
  local target_dir="$SERVER_BIN_ROOT/$(target_dir_name "$target")"

  log_step "构建后端二进制 ($goos/$goarch)"

  rm -rf "$target_dir"
  mkdir -p "$target_dir"

  (
    cd "$SERVER_DIR"
    GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$target_dir/server" ./cmd/server
    GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$target_dir/migrate" ./cmd/migrate
  )
}

print_summary() {
  local target
  local target_dir

  log_step "构建完成"
  echo "workspace 产物: $ROOT_DIR/apps/workspace/dist"
  for target in "${TARGETS[@]}"; do
    target_dir="$SERVER_BIN_ROOT/$(target_dir_name "$target")"
    echo "$target 二进制: $target_dir"
  done
}

main() {
  local target

  require_cmd pnpm
  require_cmd go

  parse_targets "$@"
  build_workspace

  for target in "${TARGETS[@]}"; do
    build_target_binaries "$target"
  done

  print_summary
}

main "$@"
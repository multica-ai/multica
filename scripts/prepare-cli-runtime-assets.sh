#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
OUT_DIR="${MULTICA_CLI_RUNTIME_ASSETS_DIR:-${REPO_ROOT}/.dist/cli-runtime}"

DEFAULT_TARGETS=(
  "darwin/amd64"
  "darwin/arm64"
  "linux/amd64"
  "linux/arm64"
  "windows/amd64"
  "windows/arm64"
)

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    printf 'error: required command not found: %s\n' "$1" >&2
    exit 1
  }
}

need_cmd node
need_cmd npm

TARGETS=("${DEFAULT_TARGETS[@]}")
if [[ -n "${MULTICA_CLI_TARGETS:-}" ]]; then
  read -r -a TARGETS <<< "${MULTICA_CLI_TARGETS}"
fi

mkdir -p "$OUT_DIR"

for target in "${TARGETS[@]}"; do
  goos="${target%/*}"
  goarch="${target#*/}"
  printf '==> preparing Claude SDK bundle for %s/%s\n' "$goos" "$goarch"
  node "${SCRIPT_DIR}/prepare-claude-sdk-bundle.mjs" "${OUT_DIR}/${goos}-${goarch}" "$goos" "$goarch"
done

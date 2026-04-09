#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_DIR="$ROOT_DIR/server"
RELEASE_ROOT="${RELEASE_ROOT:-$ROOT_DIR/dist/backend-linux-amd64}"
RELEASE_NAME="${RELEASE_NAME:-multica-backend-linux-amd64}"
RELEASE_DIR="$RELEASE_ROOT/$RELEASE_NAME"
ARCHIVE_PATH="$RELEASE_ROOT/${RELEASE_NAME}.tar.gz"

log_step() {
  printf '\n==> %s\n' "$1"
}

require_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "Missing required command: $cmd" >&2
    exit 1
  fi
}

main() {
  require_cmd go
  require_cmd tar

  rm -rf "$RELEASE_DIR"
  mkdir -p "$RELEASE_DIR/migrations" "$RELEASE_DIR/config"

  log_step "Building linux/amd64 backend binaries"
  (
    cd "$SERVER_DIR"
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$RELEASE_DIR/server" ./cmd/server
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$RELEASE_DIR/migrate" ./cmd/migrate
  )

  log_step "Assembling release directory"
  cp -R "$SERVER_DIR/migrations"/. "$RELEASE_DIR/migrations/"
  cp "$ROOT_DIR/deploy/server.env.example" "$RELEASE_DIR/config/server.env.example"

  log_step "Creating tarball"
  mkdir -p "$RELEASE_ROOT"
  tar -C "$RELEASE_ROOT" -czf "$ARCHIVE_PATH" "$RELEASE_NAME"

  log_step "Release ready"
  echo "Directory: $RELEASE_DIR"
  echo "Archive:   $ARCHIVE_PATH"
}

main "$@"
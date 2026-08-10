#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-${CONNECTOR_VERSION:-dev}}"
VERSION="${VERSION#v}"
COMMIT="${CONNECTOR_COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || printf unknown)}"
BUILD_DATE="${CONNECTOR_BUILD_DATE:-$(date -u '+%Y-%m-%dT%H:%M:%SZ')}"
OUT_DIR="${CONNECTOR_DIST_DIR:-dist/connectors/v${VERSION}}"

command -v go >/dev/null 2>&1 || {
  echo "go is required" >&2
  exit 1
}
command -v zip >/dev/null 2>&1 || {
  echo "zip is required for the Windows connector archives" >&2
  exit 1
}

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"
OUT_DIR="$(cd "$OUT_DIR" && pwd)"

targets=(
  darwin/amd64
  darwin/arm64
  linux/amd64
  linux/arm64
  windows/amd64
  windows/arm64
)

for target in "${targets[@]}"; do
  goos="${target%/*}"
  goarch="${target#*/}"
  work_dir="$(mktemp -d)"
  binary="multica"
  if [[ "$goos" == "windows" ]]; then
    binary="multica.exe"
  fi

  echo "==> Building connector ${goos}/${goarch}"
  (
    cd server
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
      -trimpath \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${BUILD_DATE}" \
      -o "$work_dir/$binary" \
      ./cmd/multica
  )

  if [[ "$goos" == "windows" ]]; then
    archive="multica-cli-${VERSION}-${goos}-${goarch}.zip"
    (cd "$work_dir" && zip -q "$OUT_DIR/$archive" "$binary")
  else
    archive="multica-cli-${VERSION}-${goos}-${goarch}.tar.gz"
    tar -czf "$OUT_DIR/$archive" -C "$work_dir" "$binary"
  fi
  rm -rf "$work_dir"
done

(
  cd "$OUT_DIR"
  sha256sum multica-cli-* > checksums.txt
)

echo "Connector release assets: $OUT_DIR"

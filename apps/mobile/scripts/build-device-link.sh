#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/../../.." && pwd)"
output="$root/apps/mobile/modules/multica-device-link/ios/MulticaLink.xcframework"
fingerprint="$root/apps/mobile/modules/multica-device-link/ios/.framework-fingerprint"
mobile_version="v0.0.0-20261011034528-197770444867"
if ! xcrun --find swiftc >/dev/null 2>&1 || ! xcrun --sdk iphoneos --show-sdk-path >/dev/null 2>&1; then
  echo "Install Xcode and select its developer directory before building the iOS device-link framework." >&2
  exit 1
fi
cd "$root/server"
current=$(shasum go.mod go.sum mobilelink/*.go internal/daemon/mobilediag/*.go internal/daemon/p2p/*.go "$root/apps/mobile/scripts/build-device-link.sh" | shasum | cut -d ' ' -f 1)
if [ -d "$output" ] && [ -f "$fingerprint" ] && [ "$(cat "$fingerprint")" = "$current" ]; then
  exit 0
fi
tools="$root/apps/mobile/.cache/go-mobile/$mobile_version"
mkdir -p "$tools"
if [ ! -x "$tools/gomobile" ] || [ ! -x "$tools/gobind" ]; then
  GOBIN="$tools" go install "golang.org/x/mobile/cmd/gomobile@$mobile_version"
  GOBIN="$tools" go install "golang.org/x/mobile/cmd/gobind@$mobile_version"
fi
# The generated binding needs x/mobile/bind. Keep this build-only dependency
# and its sums out of the server's production module.
build_dir=$(mktemp -d "${TMPDIR:-/tmp}/multica-ios-bind.XXXXXX")
trap 'rm -rf "$build_dir"' EXIT
cp go.mod "$build_dir/mobile.mod"
cp go.sum "$build_dir/mobile.sum"
go mod edit -modfile="$build_dir/mobile.mod" -require="golang.org/x/mobile@$mobile_version"
GOFLAGS="-modfile=$build_dir/mobile.mod -mod=mod" PATH="$tools:$PATH" \
  "$tools/gomobile" bind -target=ios,iossimulator -iosversion=16.4 -o "$output" ./mobilelink
printf '%s\n' "$current" > "$fingerprint"

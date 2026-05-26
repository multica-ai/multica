#!/usr/bin/env bash
# package-desktop.sh — Local, unsigned desktop builds for testing.
#
# Wraps `pnpm --filter @multica/desktop package` with sensible defaults:
#   - Defaults to the current host platform + architecture.
#   - Skips macOS notarization unless APPLE_TEAM_ID is set in the env
#     (release builds set those via CI secrets).
#   - Skips macOS code signing if no Developer ID cert is auto-discovered.
#
# Usage:
#   ./scripts/package-desktop.sh                 # host platform, host arch
#   ./scripts/package-desktop.sh mac arm64       # explicit Apple Silicon
#   ./scripts/package-desktop.sh mac x64         # explicit Intel Mac
#   ./scripts/package-desktop.sh win x64         # Windows installer
#   ./scripts/package-desktop.sh linux x64       # Linux AppImage
#   ./scripts/package-desktop.sh all             # all platforms (mac host only)
#   ./scripts/package-desktop.sh --clean mac arm64   # wipe dist/ first
#
# The --clean flag avoids leaving stale latest-*.yml files from a previous
# build of a different platform, which would mislead electron-updater into
# advertising a version that no longer has matching artifacts.

set -euo pipefail
cd "$(dirname "$0")/.."

CLEAN=0
POSITIONAL=()
for arg in "$@"; do
  case "$arg" in
    --clean) CLEAN=1 ;;
    -h|--help)
      sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) POSITIONAL+=("$arg") ;;
  esac
done

PLATFORM="${POSITIONAL[0]:-host}"
ARCH="${POSITIONAL[1]:-host}"

# Host detection
case "$(uname -s)" in
  Darwin) HOST_PLATFORM=mac ;;
  Linux) HOST_PLATFORM=linux ;;
  *) echo "Unsupported host OS: $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  arm64|aarch64) HOST_ARCH=arm64 ;;
  x86_64) HOST_ARCH=x64 ;;
  *) echo "Unsupported host arch: $(uname -m)" >&2; exit 1 ;;
esac

[[ "$PLATFORM" == "host" ]] && PLATFORM="$HOST_PLATFORM"
[[ "$ARCH" == "host" ]] && ARCH="$HOST_ARCH"

# Local unsigned build: bypass Apple Developer cert auto-discovery so the
# build doesn't fail with "no identity found". Notarization is also gated
# on APPLE_TEAM_ID inside scripts/package.mjs.
if [[ -z "${APPLE_TEAM_ID:-}" ]]; then
  export CSC_IDENTITY_AUTO_DISCOVERY=false
fi

if [[ "$CLEAN" == 1 ]]; then
  echo "→ cleaning apps/desktop/dist/"
  rm -rf apps/desktop/dist
fi

# Compose forwarded args. scripts/package.mjs accepts --mac/--win/--linux
# (or --all-platforms) plus --arm64/--x64.
FORWARD=()
if [[ "$PLATFORM" == "all" ]]; then
  FORWARD+=(--all-platforms)
else
  FORWARD+=("--$PLATFORM")
  if [[ "$ARCH" == "all" ]]; then
    FORWARD+=(--arm64 --x64)
  else
    FORWARD+=("--$ARCH")
  fi
fi

echo "→ Building $PLATFORM $ARCH (unsigned local build)"
echo "  pnpm --filter @multica/desktop package -- ${FORWARD[*]}"
echo ""
pnpm --filter @multica/desktop package -- "${FORWARD[@]}"

echo ""
echo "✓ Artifacts in apps/desktop/dist/:"
for f in apps/desktop/dist/multica-desktop-*; do
  [[ -e "$f" ]] || continue
  case "$f" in
    *.blockmap) continue ;;
  esac
  printf "  %-12s %s\n" "$(du -h "$f" | awk '{print $1}')" "$f"
done

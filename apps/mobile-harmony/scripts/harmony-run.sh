#!/usr/bin/env bash
# Build and run the HarmonyOS app through the DevEco Studio toolchain.
#
# Locates the hvigor/ohpm/hdc binaries bundled with DevEco Studio (no global
# installs required), restores ohpm dependencies (the RNOH platform HAR is a
# file: dependency inside the npm packages, so this is fast once cached),
# builds the debug HAP with hvigor, installs it with hdc, reverse-forwards
# the device's Metro port to this Mac, and launches the app.
#
# MULTICA_API_URL / MULTICA_WEB_URL come from the calling package.json
# script's dotenv-cli so Metro inlines the same variant into the bundle.
# METRO_PORT defaults to 8082 so a harmony Metro can coexist with an iOS
# Metro already listening on 8081.
#
# The native tree is committed source; RNOHPackagesFactory.ets,
# autolinking.cmake and cpp/generated are build outputs regenerated on every
# hvigor run and gitignored — never edit them by hand.
set -euo pipefail

DEVECO_HOME="${DEVECO_HOME:-/Applications/DevEco-Studio.app}"
DEVECO_NODE="$DEVECO_HOME/Contents/tools/node/bin/node"
HVIGRW="$DEVECO_HOME/Contents/tools/hvigor/bin/hvigorw.js"
OHPM="$DEVECO_HOME/Contents/tools/ohpm/bin/ohpm"
HDC="$DEVECO_HOME/Contents/sdk/default/openharmony/toolchains/hdc"

for tool in "$DEVECO_NODE" "$HVIGRW" "$OHPM" "$HDC"; do
  if [ ! -e "$tool" ]; then
    echo "Missing DevEco tool: $tool" >&2
    echo "Install DevEco Studio or set DEVECO_HOME to its location." >&2
    exit 1
  fi
done

PROJECT_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
HARMONY_DIR="$PROJECT_ROOT/harmony"
BUNDLE_NAME="${HARMONY_BUNDLE_NAME:-ai.multica.mobile.harmony}"
METRO_PORT="${METRO_PORT:-8082}"
# "release" = self-contained: a dev-mode plain-JS bundle is embedded in the
# HAP's rawfile, so the app starts with no Metro on the Mac (trying it out,
# demoing) and transparently upgrades to live Metro when one is running.
# The build stays hvigor-debug on purpose: the release ArkTS runtime calls
# runApplication ahead of rawfile-bundle registration and white-screens
# (see apps/mobile-harmony/AGENTS.md, "Embedded bundles"). The default
# debug build relies on Metro for every launch.

export DEVECO_SDK_HOME="${DEVECO_SDK_HOME:-$DEVECO_HOME/Contents/sdk}"
# hvigor's HAP packaging shells out to a Java signing tool.
export JAVA_HOME="${JAVA_HOME:-$DEVECO_HOME/Contents/jbr/Contents/Home}"

if [ "${1:-}" = "--embed" ] || [ "${HARMONY_BUILD_MODE:-}" = "release" ]; then
  if [ -z "${MULTICA_API_URL:-}" ]; then
    echo "MULTICA_API_URL is not set; embedded builds bake it into the bundle." >&2
    echo "Run via the package.json harmony:* scripts (dotenv-cli) or export it." >&2
    exit 1
  fi
  # Plain dev bundle (NOT Hermes bytecode, NOT --dev false): rawfile HBC and
  # minified bundles both fail to register in the RNOH 0.77 runtime; the dev
  # plain bundle with inlineRequires:true is the one verified combination.
  # inlineRequires is pinned in metro.config.js — keep them in sync.
  # --reset-cache: metro's transform cache does not key on env vars, so
  # switching variants (staging <-> production) without it bakes the PREVIOUS
  # variant's MULTICA_API_URL into the embedded bundle.
  (cd "$PROJECT_ROOT" && "$PROJECT_ROOT/node_modules/.bin/react-native" \
    bundle-harmony \
    --dev true \
    --reset-cache \
    --bundle-output "$HARMONY_DIR/entry/src/main/resources/rawfile/bundle.harmony.js")
fi

# ohpm install is idempotent and cached; it must run before hvigor so the
# RNOH platform and oh-tpl HARs are linked into harmony/oh_modules.
(cd "$HARMONY_DIR" && "$OHPM" install)

(cd "$HARMONY_DIR" && "$DEVECO_NODE" "$HVIGRW" \
  --mode module \
  -p module=entry@default \
  -p product=default \
  -p buildMode=debug \
  -p requiredDeviceType=phone \
  assembleHap \
  --no-daemon)

HAP=$(ls "$HARMONY_DIR"/entry/build/default/outputs/default/entry-default-unsigned.hap)
"$HDC" install -r "$HAP"

# The device-side Metro client always targets localhost:8081; map it to this
# Mac's Metro port. A stale mapping for 8081 is removed first so re-running
# after a Metro port change reconnects cleanly.
"$HDC" fport rm tcp:8081 tcp:8081 >/dev/null 2>&1 || true
"$HDC" fport rm tcp:8081 tcp:8082 >/dev/null 2>&1 || true
"$HDC" rport tcp:8081 "tcp:$METRO_PORT"

# Fresh launch: force-stop first so a running instance reloads the new HAP.
"$HDC" shell "aa force-stop $BUNDLE_NAME" >/dev/null 2>&1 || true
"$HDC" shell "aa start -a EntryAbility -b $BUNDLE_NAME"

echo ""
echo "HAP installed and app launched."
if [ "${1:-}" = "--embed" ] || [ "${HARMONY_BUILD_MODE:-}" = "release" ]; then
  echo "Embedded bundle: the app runs standalone; start Metro anytime to hot-reload it."
else
  echo "No embedded bundle: start Metro before launching or the app white-screens:"
  echo "  cd $PROJECT_ROOT && pnpm dev:staging -- --port $METRO_PORT"
fi

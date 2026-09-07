#!/usr/bin/env bash
# Build and run the iOS app, re-applying app.config.ts to ios/ first.
#
# `expo run:ios` only prebuilds when ios/ is missing (ensureNativeProjectAsync
# in @expo/cli): when the directory already exists it returns early and config
# plugins are never re-applied. Everything driven by app.config.ts — app icon,
# bundle identifier, display name, URL scheme, Info.plist permission strings —
# then stays frozen at whatever the first prebuild produced, while the build
# still reports success.
#
# ios/ is gitignored and fully generated, so prebuilding on every run is safe
# and idempotent. --no-install is fine because run:ios installs pods itself
# when Podfile.lock is out of date; --clean is deliberately avoided since
# expo-build-properties sets buildReactNativeFromSource, making a from-source
# pod rebuild far too slow for the normal edit/run loop.
#
# APP_ENV and the .env file are supplied by the calling package.json script, so
# prebuild and run resolve the same variant. Arguments are forwarded to
# run:ios only — prebuild takes the same flags for every variant.
set -euo pipefail

pnpm exec expo prebuild -p ios --no-install
exec pnpm exec expo run:ios "$@"

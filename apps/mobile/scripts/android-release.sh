#!/usr/bin/env bash
# Build a standalone Android Release APK after applying the selected Expo config.
# The package script supplies APP_ENV and the corresponding public API env.
set -euo pipefail

pnpm exec expo prebuild -p android --no-install
exec ./android/gradlew -p android assembleRelease "$@"

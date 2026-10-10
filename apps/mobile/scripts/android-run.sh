#!/usr/bin/env bash
# Build and run the Android app, re-applying app.config.ts to android/ first.
# Generated Android project files are gitignored; prebuild is idempotent and
# keeps application ID, SDK floor, plugins, and display name aligned per env.
set -euo pipefail

pnpm exec expo prebuild -p android --no-install
exec pnpm exec expo run:android "$@"

#!/usr/bin/env bash
set -euo pipefail

paths=(
  apps/desktop/src/main/cli-bootstrap.ts
  apps/desktop/src/main/updater.ts
  apps/desktop/src/main/cerebro-distribution.ts
  apps/desktop/electron-builder.yml
  packages/views/runtimes/components/update-section.tsx
)

if rg -n 'multica-ai/multica' "${paths[@]}"; then
  echo "Cerebro distribution paths must not reference multica-ai/multica." >&2
  exit 1
fi

if rg -n 'defaultRepo\s*=\s*"multica-ai/multica"' server/internal/cerebro/forkdist/forkdist.go; then
  echo "The server update repository must remain Firtal-owned." >&2
  exit 1
fi

echo "Cerebro distribution boundary verified."

#!/usr/bin/env bash
set -euo pipefail

# Wait for Docker to be ready (Docker Desktop starts after login).
MAX_WAIT=120
WAITED=0
until docker info > /dev/null 2>&1; do
  if [ $WAITED -ge $MAX_WAIT ]; then
    echo "Docker not ready after ${MAX_WAIT}s, giving up." >&2
    exit 1
  fi
  sleep 2
  WAITED=$((WAITED + 2))
done

cd /Users/hvejsel/code/multica
exec make start

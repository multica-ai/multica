#!/usr/bin/env bash
# Start the local CEO workbench (browser UI for portfolio queue + dispatch).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
HOST="${CEO_WORKBENCH_HOST:-127.0.0.1}"
PORT="${CEO_WORKBENCH_PORT:-9477}"
OPEN_BROWSER="${CEO_WORKBENCH_OPEN_BROWSER:-1}"

if ! command -v python3 &>/dev/null; then
  echo "error: python3 is required" >&2
  exit 1
fi

if ! command -v gh &>/dev/null; then
  echo "error: gh CLI is required" >&2
  exit 1
fi

export REGISTRY="${REGISTRY:-$MULTICA_ROOT/.ai-company/templates/project-registry.yaml}"
export GITHUB_ORG="${GITHUB_ORG:-chenzh}"
export CEO_WORKBENCH_HOST="$HOST"
export CEO_WORKBENCH_PORT="$PORT"

url="http://$HOST:$PORT"

if [ "$OPEN_BROWSER" = "1" ] && command -v open &>/dev/null; then
  (sleep 1 && open "$url") &
fi

echo "AI 公司工作台: $url"
echo "Registry: $REGISTRY"
exec python3 "$SCRIPT_DIR/ceo-workbench-server.py"

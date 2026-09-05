#!/usr/bin/env bash
# CEO approval bridge CLI — GitHub BLOCKED ↔ Multica ↔ Feishu.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=lib/source-local-env.sh
source "$SCRIPT_DIR/lib/source-local-env.sh"

REGISTRY="${REGISTRY:-$MULTICA_ROOT/.ai-company/templates/project-registry.yaml}"
GITHUB_ORG="${GITHUB_ORG:-chenzh}"
FRONTEND_URL="${MULTICA_FRONTEND_URL:-http://localhost:3000}"

exec python3 "$SCRIPT_DIR/lib/approval_bridge.py" \
  --registry "$REGISTRY" \
  --github-org "$GITHUB_ORG" \
  --frontend-url "$FRONTEND_URL" \
  "$@"

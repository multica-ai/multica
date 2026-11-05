#!/usr/bin/env bash
set -euo pipefail
verify_repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
verify_tools_root="${MULTICA_VERIFICATION_TOOLS_ROOT:-$HOME/.multica/verification-tools}"
verify_node="$verify_tools_root/node/bin/node"
if [ ! -x "$verify_node" ] || [ ! -f "$verify_repo_root/packages/verification/dist/mcp/stdio.js" ]; then
  echo "Run bash scripts/verification/bootstrap.sh before starting this MCP host." >&2
  exit 2
fi
export PATH="$verify_tools_root/node/bin:$PATH"
exec "$verify_node" "$verify_repo_root/packages/verification/dist/mcp/stdio.js" --project "$verify_repo_root" "$@"

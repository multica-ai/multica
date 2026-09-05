#!/usr/bin/env bash
# Push PORTFOLIO_GH_TOKEN from local.env to chenzh/multica GitHub Actions secret.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=lib/source-local-env.sh
source "$SCRIPT_DIR/lib/source-local-env.sh"

ORG="${GITHUB_ORG:-chenzh}"
REPO="${PORTFOLIO_REPO:-$ORG/multica}"
TOKEN="${PORTFOLIO_GH_TOKEN:-${GH_TOKEN:-}}"

if [ -z "$TOKEN" ] || [[ "$TOKEN" == github_pat_YOUR_* ]] || [[ "$TOKEN" == YOUR_* ]]; then
  echo "error: set PORTFOLIO_GH_TOKEN in .ai-company/config/local.env" >&2
  exit 1
fi

echo "Setting PORTFOLIO_GH_TOKEN on $REPO ..."
printf '%s' "$TOKEN" | gh secret set PORTFOLIO_GH_TOKEN -R "$REPO"
echo "Done. Used by: .github/workflows/portfolio-agent-dispatch.yml"

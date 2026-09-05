#!/usr/bin/env bash
# Print multica autopilot create commands for each project in project-registry.yaml.
set -euo pipefail

MULTICA_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
REGISTRY="${REGISTRY:-$MULTICA_ROOT/.ai-company/templates/project-registry.yaml}"
AGENT_ID="${MULTICA_DEV_AGENT_ID:-<YOUR_RUNTIME_AGENT_UUID>}"
CRON="${MULTICA_AUTOPILOT_CRON:-0 2 * * *}"
TZ="${MULTICA_AUTOPILOT_TZ:-Asia/Shanghai}"

if [ ! -f "$REGISTRY" ]; then
  echo "error: registry not found: $REGISTRY" >&2
  exit 1
fi

echo "# Paste after: multica login && export MULTICA_DEV_AGENT_ID=..."
echo "# Agent id from: multica agent list"
echo ""

current_id="" current_slug="" current_repo=""

flush() {
  if [ -z "$current_id" ]; then return; fi
  slug="${current_slug:-$current_id}"
  cat <<EOF
# --- $current_id ($current_repo) ---
multica autopilot create \\
  --title "Nightly — $current_id" \\
  --description "Repo: $current_repo. Read .delivery/$slug/ brief and accept_cases. Process GitHub issues labeled agent-safe. Follow .delivery/README.md verifier gate (exit code 0)." \\
  --agent $AGENT_ID \\
  --mode create_issue

# Then:
# multica autopilot trigger-add <AUTOPILOT_ID> --kind schedule --cron "$CRON" --timezone $TZ

EOF
  current_id=""
  current_slug=""
  current_repo=""
}

while IFS= read -r line; do
  line="$(echo "$line" | sed 's/^[[:space:]]*//')"
  [[ "$line" =~ ^-\ id:\ (.+)$ ]] && { flush; current_id="${BASH_REMATCH[1]}"; continue; }
  [[ "$line" =~ ^repo:\ (.+)$ ]] && { current_repo="${BASH_REMATCH[1]}"; continue; }
  [[ "$line" =~ ^delivery_slug:\ (.+)$ ]] && { current_slug="${BASH_REMATCH[1]}"; continue; }
done <"$REGISTRY"
flush

echo "# Portfolio GHA alternative (no Multica cron):"
echo "# bash scripts/ai-company/portfolio-dispatch.sh --max-total 5"

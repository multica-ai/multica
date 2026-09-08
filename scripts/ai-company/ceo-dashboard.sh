#!/usr/bin/env bash
# CEO dashboard — one command summary across all projects in project-registry.yaml.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=lib/source-local-env.sh
source "$SCRIPT_DIR/lib/source-local-env.sh"
REGISTRY="${REGISTRY:-$MULTICA_ROOT/.ai-company/templates/project-registry.yaml}"
GITHUB_ORG="${GITHUB_ORG:-chenzh}"
SINCE="${SINCE:-@yesterday}"
JSON=0
DISPATCH=0
MAX_TOTAL="${MAX_TOTAL:-5}"

usage() {
  cat <<'EOF'
Usage: ceo-dashboard.sh [options]

Summarize agent-safe queue, BLOCKED, running, and recent cursor/* merges
for every project in project-registry.yaml.

Options:
  --registry PATH    project-registry.yaml
  --org ORG          Replace your-org/ prefix in registry repos (default: chenzh)
  --since DATE       gh search date for merged PRs (default: @yesterday)
  --json             Machine-readable JSON lines
  --dispatch         After summary, run portfolio-dispatch.sh
  --max-total N      With --dispatch (default: 5)
  -h, --help

Example:
  bash scripts/ai-company/ceo-dashboard.sh
  bash scripts/ai-company/ceo-dashboard.sh --org chenzh --dispatch
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --registry) REGISTRY="${2:?}"; shift 2 ;;
    --org) GITHUB_ORG="${2:?}"; shift 2 ;;
    --since) SINCE="${2:?}"; shift 2 ;;
    --json) JSON=1; shift ;;
    --dispatch) DISPATCH=1; shift ;;
    --max-total) MAX_TOTAL="${2:?}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

if ! command -v gh &>/dev/null; then
  echo "error: gh CLI required" >&2
  exit 1
fi

if [ ! -f "$REGISTRY" ]; then
  echo "error: registry not found: $REGISTRY" >&2
  exit 1
fi

resolve_repo() {
  local raw="$1"
  raw="${raw#github.com/}"
  raw="${raw#https://github.com/}"
  if [[ "$raw" == your-org/* ]]; then
    raw="${raw/your-org/$GITHUB_ORG}"
  fi
  echo "$raw"
}

declare -a IDS=() REPOS=() PAUSED=() CAPS=() PRIORITIES=()
current_id="" current_repo="" current_paused="false" current_cap="1" current_priority="0"

flush() {
  [ -z "$current_id" ] && return
  IDS+=("$current_id")
  REPOS+=("$(resolve_repo "$current_repo")")
  PAUSED+=("$current_paused")
  CAPS+=("$current_cap")
  PRIORITIES+=("$current_priority")
  current_id=""
}
while IFS= read -r line; do
  line="$(echo "$line" | sed 's/^[[:space:]]*//')"
  [[ "$line" =~ ^-\ id:\ (.+)$ ]] && { flush; current_id="${BASH_REMATCH[1]}"; continue; }
  [[ "$line" =~ ^repo:\ (.+)$ ]] && { current_repo="${BASH_REMATCH[1]}"; continue; }
  [[ "$line" =~ ^paused:\ (.+)$ ]] && { current_paused="${BASH_REMATCH[1]}"; continue; }
  [[ "$line" =~ ^max_nightly_tickets:\ (.+)$ ]] && { current_cap="${BASH_REMATCH[1]}"; continue; }
  [[ "$line" =~ ^priority:\ (.+)$ ]] && { current_priority="${BASH_REMATCH[1]}"; continue; }
done <"$REGISTRY"
flush

total_blocked=0
total_running=0
total_safe=0
total_merged=0
needs_action=0

emit_json() {
  printf '{"id":"%s","repo":"%s","paused":%s,"blocked":%s,"running":%s,"agent_safe":%s,"merged_prs":%s,"accessible":%s}\n' \
    "$1" "$2" "$3" "$4" "$5" "$6" "$7" "$8"
}

if [ "$JSON" -eq 0 ]; then
  echo "╔══════════════════════════════════════════════════════════════╗"
  printf "║  AI 公司 CEO 仪表盘  %-40s ║\n" "$(date '+%Y-%m-%d %H:%M')"
  printf "║  org: %-54s ║\n" "$GITHUB_ORG"
  echo "╚══════════════════════════════════════════════════════════════╝"
  echo ""
fi

for i in "${!IDS[@]}"; do
  id="${IDS[$i]}"
  repo="${REPOS[$i]}"
  paused="${PAUSED[$i]}"
  accessible=true

  if ! gh repo view "$repo" &>/dev/null; then
    accessible=false
    blocked=0 running=0 safe=0 merged=0
  else
    blocked="$(gh issue list -R "$repo" -l agent-blocked -s open --json number -q 'length' 2>/dev/null || echo 0)"
    running="$(gh issue list -R "$repo" -l agent-running -s open --json number -q 'length' 2>/dev/null || echo 0)"
    safe="$(gh issue list -R "$repo" --label agent-safe --state open --json labels \
      --jq '[.[] | select([.labels[].name] | (index("agent-running") | not) and (index("agent-blocked") | not) and (index("agent-done") | not))] | length' 2>/dev/null || echo 0)"
    merged="$(gh pr list -R "$repo" -s merged --search "head:cursor/ merged:>$SINCE" --json number -q 'length' 2>/dev/null || echo 0)"
  fi

  blocked="${blocked:-0}"
  running="${running:-0}"
  safe="${safe:-0}"
  merged="${merged:-0}"

  if [ "$paused" != "true" ]; then
    total_blocked=$((total_blocked + blocked))
    total_running=$((total_running + running))
    total_safe=$((total_safe + safe))
    total_merged=$((total_merged + merged))
  fi
  [ "$blocked" -gt 0 ] && [ "$paused" != "true" ] && needs_action=1

  if [ "$JSON" -eq 1 ]; then
    emit_json "$id" "$repo" "$paused" "${blocked:-0}" "${running:-0}" "${safe:-0}" "${merged:-0}" "$accessible"
    continue
  fi

  status="🟢"
  [ "$paused" = "true" ] && status="⏸️ "
  [ "$accessible" = false ] && status="⚪"
  [ "${blocked:-0}" -gt 0 ] && status="🔴"

  printf "%s %-22s %-40s\n" "$status" "$id" "$repo"
  if [ "$accessible" = false ]; then
    echo "     repo not found — run bootstrap-project.sh --create-repo"
  else
    echo "     BLOCKED: $blocked  RUNNING: $running  QUEUE(agent-safe): $safe  MERGED($SINCE): $merged  cap/night: ${CAPS[$i]}"
    if [ "${blocked:-0}" -gt 0 ]; then
      gh issue list -R "$repo" -l agent-blocked -s open --json number,title,url \
        -q '.[] | "     → #\(.number) \(.title)\n       \(.url)"' 2>/dev/null || true
    fi
  fi
  echo ""
done

if [ "$JSON" -eq 0 ]; then
  echo "────────────────────────────────────────────────────────────────"
  printf "TOTAL  BLOCKED: %-3s  RUNNING: %-3s  QUEUE: %-3s  MERGED: %-3s\n" \
    "$total_blocked" "$total_running" "$total_safe" "$total_merged"
  echo ""

  if [ "$needs_action" -eq 1 ]; then
    echo "⚠️  ACTION: 处理 BLOCKED → runbooks/blocked-triage.md"
  elif [ "$total_safe" -eq 0 ] && [ "$needs_action" -eq 0 ]; then
    echo "✅ 无 BLOCKED、队列已空 — 可躺平（从 backlog 补票见 sync-backlog-to-issues.sh）"
  elif [ "$total_merged" -eq 0 ] && [ "$total_safe" -gt 0 ]; then
    echo "💤 队列有粮但未交付 — 考虑: ceo-dashboard.sh --dispatch"
  else
    echo "✅ 无 BLOCKED — 可躺平（仍建议抽检 1 条 merge）"
  fi
  echo ""
  echo "Commands:"
  echo "  bash scripts/ai-company/portfolio-dispatch.sh --local --max-total $MAX_TOTAL"
  echo "  bash scripts/ai-company/ceo-dashboard.sh --dispatch"
  echo ""
  bash "$SCRIPT_DIR/multica-runtime-status.sh" --human 2>/dev/null || true
fi

if [ "$DISPATCH" -eq 1 ]; then
  echo ""
  if ! command -v cursor-agent &>/dev/null || ! cursor-agent status &>/dev/null; then
    echo "error: cursor-agent not logged in — run: cursor-agent login" >&2
    exit 1
  fi
  dispatch_args=(--registry "$REGISTRY" --max-total "$MAX_TOTAL" --local)
  echo ">> portfolio-dispatch --local --max-total $MAX_TOTAL (cursor-agent session)"
  bash "$MULTICA_ROOT/scripts/ai-company/portfolio-dispatch.sh" "${dispatch_args[@]}"
fi

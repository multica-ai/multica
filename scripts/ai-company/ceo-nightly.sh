#!/usr/bin/env bash
# Nightly CEO routine: optional dispatch, then daily brief + notify (21:00 cron).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=lib/source-local-env.sh
source "$SCRIPT_DIR/lib/source-local-env.sh"

REGISTRY="${REGISTRY:-$MULTICA_ROOT/.ai-company/templates/project-registry.yaml}"
GITHUB_ORG="${GITHUB_ORG:-chenzh}"
MAX_TOTAL="${MAX_TOTAL:-5}"
DISPATCH="${CEO_NIGHTLY_DISPATCH:-1}"
DISPATCH_BG="${CEO_NIGHTLY_DISPATCH_BG:-1}"
DISPATCH_LOG="${CEO_NIGHTLY_DISPATCH_LOG:-$HOME/.multica/ceo-nightly-dispatch.log}"
AUTO_MERGE="${CEO_AUTO_MERGE:-1}"
SYNC_BACKLOG="${CEO_SYNC_BACKLOG:-1}"
BRIEF=1
SINCE="${SINCE:-@yesterday}"

usage() {
  cat <<'EOF'
Usage: ceo-nightly.sh [options]

Typical cron (21:00 Asia/Shanghai):
  0 21 * * * cd ~/Projects/multica && bash scripts/ai-company/ceo-nightly.sh >> ~/.multica/ceo-nightly.log 2>&1

Options:
  --dispatch          Force portfolio dispatch before brief
  --no-dispatch       Brief only
  --brief-only        Alias for --no-dispatch
  --sync-dispatch     Wait for dispatch to finish (blocks brief until agents done)
  --max-total N       Dispatch cap (default: 5)
  --registry PATH
  --org ORG
  -h, --help

Environment:
  CEO_NIGHTLY_DISPATCH=1|0       default 1
  CEO_NIGHTLY_DISPATCH_BG=1|0    default 1 — background dispatch so brief fires immediately
  CEO_AUTO_MERGE=1|0             default 1 — merge green open PRs before dispatch
  CEO_RECONCILE_QUEUE=1|0        default 1 — fix stale agent-* labels
  CEO_SYNC_BACKLOG=1|0           default 1 — sync missing backlog tickets before dispatch
  SLACK_WEBHOOK_URL / FEISHU_WEBHOOK_URL in local.env
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --dispatch) DISPATCH=1; shift ;;
    --no-dispatch|--brief-only) DISPATCH=0; shift ;;
    --sync-dispatch) DISPATCH_BG=0; shift ;;
    --max-total) MAX_TOTAL="${2:?}"; shift 2 ;;
    --registry) REGISTRY="${2:?}"; shift 2 ;;
    --org) GITHUB_ORG="${2:?}"; shift 2 ;;
    --since) SINCE="${2:?}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

reconcile_queue() {
  bash "$SCRIPT_DIR/ceo-reconcile-queue.sh" \
    --registry "$REGISTRY" \
    --org "$GITHUB_ORG" || {
    echo "warn: reconcile failed (continuing)" >&2
  }
}

echo "=== ceo-nightly $(date -Iseconds) ==="

if launchctl print "gui/$(id -u)/com.multica.ceo-feishu-cloudflare" &>/dev/null; then
  bash "$SCRIPT_DIR/ceo-feishu-cloudflare-tunnel.sh" refresh-quick-url >>"$HOME/.multica/ceo-feishu-cloudflare-url-refresh.log" 2>&1 || true
fi

if [ "$AUTO_MERGE" -eq 1 ] || [ "${CEO_RECONCILE_QUEUE:-1}" -eq 1 ]; then
  echo ">> reconcile queue labels (pre-merge)"
  reconcile_queue
fi

if [ "$AUTO_MERGE" -eq 1 ]; then
  echo ">> auto-merge green PRs"
  bash "$SCRIPT_DIR/ceo-auto-merge.sh" \
    --registry "$REGISTRY" \
    --org "$GITHUB_ORG" || {
    echo "warn: auto-merge failed (continuing)" >&2
  }
  if [ "${CEO_RECONCILE_QUEUE:-1}" -eq 1 ]; then
    echo ">> reconcile queue labels (post-merge)"
    reconcile_queue
  fi
fi

if [ "$SYNC_BACKLOG" -eq 1 ]; then
  echo ">> sync portfolio backlogs (skip-existing)"
  bash "$SCRIPT_DIR/sync-portfolio-backlogs.sh" \
    --registry "$REGISTRY" \
    --org "$GITHUB_ORG" || {
    echo "warn: sync-portfolio-backlogs failed (continuing)" >&2
  }
fi

if [ "$DISPATCH" -eq 1 ]; then
  dispatch_cmd=(
    bash "$SCRIPT_DIR/ceo-dashboard.sh"
    --registry "$REGISTRY"
    --org "$GITHUB_ORG"
    --dispatch
    --max-total "$MAX_TOTAL"
  )
  if [ "$DISPATCH_BG" -eq 1 ]; then
    mkdir -p "$(dirname "$DISPATCH_LOG")"
    echo ">> dispatch (max_total=$MAX_TOTAL) background → $DISPATCH_LOG"
    nohup "${dispatch_cmd[@]}" >>"$DISPATCH_LOG" 2>&1 &
    echo "dispatch pid: $!"
  else
    echo ">> dispatch (max_total=$MAX_TOTAL) sync"
    "${dispatch_cmd[@]}" || {
      echo "warn: dispatch failed (continuing to brief)" >&2
    }
  fi
fi

if [ "$BRIEF" -eq 1 ]; then
  echo ">> daily brief"
  bash "$SCRIPT_DIR/ceo-daily-brief.sh" \
    --registry "$REGISTRY" \
    --org "$GITHUB_ORG" \
    --since "$SINCE" \
    --quiet
fi

echo "=== done ==="

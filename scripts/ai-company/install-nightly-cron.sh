#!/usr/bin/env bash
# Install or show crontab line for 21:00 CEO nightly (dispatch + brief).
set -euo pipefail

MULTICA_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
HOUR="${CEO_NIGHTLY_HOUR:-21}"
MINUTE="${CEO_NIGHTLY_MINUTE:-0}"
LOG="${CEO_NIGHTLY_LOG:-$HOME/.multica/ceo-nightly.log}"
INSTALL=0

usage() {
  cat <<EOF
Usage: install-nightly-cron.sh [--install]

Adds a cron job at ${HOUR}:${MINUTE} (server local time) to run ceo-nightly.sh.

Before install:
  1. cp .ai-company/config/local.env.example .ai-company/config/local.env
  2. cp .ai-company/config/proxy.env.example .ai-company/config/proxy.env   # if GitHub needs proxy
  3. Set FEISHU_WEBHOOK_URL or SLACK_WEBHOOK_URL (optional)
  4. Ensure cursor-agent logged in for local dispatch

Cron line (copy/paste or --install):
  ${MINUTE} ${HOUR} * * * cd ${MULTICA_ROOT} && bash scripts/ai-company/ceo-nightly.sh >> ${LOG} 2>&1

Manual test:
  bash scripts/ai-company/ceo-nightly.sh --no-dispatch
  bash scripts/ai-company/ceo-daily-brief.sh
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --install) INSTALL=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

CRON_LINE="${MINUTE} ${HOUR} * * * cd ${MULTICA_ROOT} && bash scripts/ai-company/ceo-nightly.sh >> ${LOG} 2>&1"

echo "Suggested crontab entry:"
echo "$CRON_LINE"
echo ""
echo "Log: $LOG"

if [ "$INSTALL" -eq 0 ]; then
  exit 0
fi

mkdir -p "$(dirname "$LOG")"
MARKER="# multica-ai-company-nightly"
existing="$(crontab -l 2>/dev/null || true)"
if echo "$existing" | grep -q "$MARKER"; then
  echo "crontab: entry already present (skip)" >&2
  exit 0
fi

{
  echo "$existing"
  echo "$MARKER"
  echo "$CRON_LINE"
} | crontab -

echo "crontab: installed" >&2

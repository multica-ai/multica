#!/usr/bin/env bash
# Post to Slack when agent delivery is blocked (optional).
set -euo pipefail

MESSAGE="${1:?usage: notify-slack.sh <message>}"
WEBHOOK="${SLACK_WEBHOOK_URL:?SLACK_WEBHOOK_URL is required}"

curl -sS -X POST "$WEBHOOK" \
  -H 'Content-Type: application/json' \
  -d "$(jq -n --arg text "$MESSAGE" '{text: $text}')"

#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

required_constants=(
  "EventCommentCreated.*comment:created"
  "EventWorkspaceUpdated.*workspace:updated"
  "EventMemberAdded.*member:added"
  "EventMemberUpdated.*member:updated"
  "EventMemberRemoved.*member:removed"
)

for pattern in "${required_constants[@]}"; do
  if ! rg -q "$pattern" "$root/server/pkg/protocol/events.go"; then
    echo "missing or renamed team-app event constant: $pattern" >&2
    exit 1
  fi
done

declare -A event_constants=(
  [EventCommentCreated]="comment:created"
  [EventWorkspaceUpdated]="workspace:updated"
  [EventMemberAdded]="member:added"
  [EventMemberUpdated]="member:updated"
  [EventMemberRemoved]="member:removed"
)

for constant in "${!event_constants[@]}"; do
  event="${event_constants[$constant]}"
  matches="$(rg -n -B 2 "h\\.publish\\(protocol\\.${constant}\\b" "$root/server/internal/handler" || true)"
  if [[ -z "$matches" ]]; then
    echo "missing emit site for $event ($constant)" >&2
    exit 1
  fi
  if ! grep -q "TEAM_APP_INTEGRATION: $event" <<<"$matches"; then
    echo "emit site for $event lacks TEAM_APP_INTEGRATION guard comment" >&2
    exit 1
  fi
done

echo "team-app event contract guard passed"

#!/usr/bin/env bash
set -euo pipefail

# ==========================================================================
# CI guard: lock the WS event constants the standalone team-app subscriber
# depends on. A rename or value change to any of these constants would
# silently break team.multica.uittai.com's WebSocket subscriber, which
# consumes the event names verbatim. Run on every PR via .github/workflows
# and `make check`.
#
# Story: M-PR#2 (verification-only event-contract lock)
# ==========================================================================

EVENTS_FILE="server/pkg/protocol/events.go"

if [ ! -f "$EVENTS_FILE" ]; then
  echo "ERROR: $EVENTS_FILE not found (run from repo root)" >&2
  exit 1
fi

# Pairs of (Go const name | expected string value). Any rename, removal, or
# value mutation must fail the build.
required=(
  "EventCommentCreated|comment:created"
  "EventWorkspaceUpdated|workspace:updated"
  "EventMemberAdded|member:added"
  "EventMemberUpdated|member:updated"
  "EventMemberRemoved|member:removed"
)

fail=0
for pair in "${required[@]}"; do
  name="${pair%%|*}"
  value="${pair##*|}"

  # Assert the `name = "value"` line exists (whitespace-tolerant).
  if ! grep -Eq "^[[:space:]]*${name}[[:space:]]*=[[:space:]]*\"${value}\"[[:space:]]*$" "$EVENTS_FILE"; then
    echo "ERROR: ${name} = \"${value}\" missing or modified in $EVENTS_FILE — required by team-app integration (M-PR#2)" >&2
    fail=1
    continue
  fi

  # Assert a TEAM_APP_INTEGRATION marker appears within the 3 lines preceding
  # the constant. Marker must stay co-located so future maintainers see it
  # before they edit the constant.
  if ! awk -v target="${name}" '
    /TEAM_APP_INTEGRATION/ { mark = NR }
    $0 ~ ("^[[:space:]]*" target "[[:space:]]*=") {
      if (mark > 0 && NR - mark <= 3) { found = 1; exit }
    }
    END { exit found ? 0 : 1 }
  ' "$EVENTS_FILE"; then
    echo "ERROR: ${name} missing // TEAM_APP_INTEGRATION marker (within 3 lines above) in $EVENTS_FILE" >&2
    fail=1
  fi
done

if [ "$fail" -ne 0 ]; then
  echo "" >&2
  echo "Coordinate any change with the team.multica.uittai.com integration owner before merging." >&2
  exit 1
fi

echo "✅ team-app integration event contract intact"

#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

forbidden='cerebro_round_(run|run_item|held_trigger)|held_trigger|held reply|batch_tool_mode|RoundTaskContext|RoundRelevanceCheck|roundIssueIdsToExclude|useDismissRound|/dismiss|cycle_opened_at|active_run|schedule_cron'
implementation=(
  packages/cerebro-rounds
  packages/cerebro-inbox
  packages/cerebro-inbox-dynamic
  packages/views/issues/components/issue-detail.tsx
  server/internal/cerebro/rounds
  server/internal/cerebro/runtime
  server/internal/daemon
  server/internal/handler
  server/internal/service/task.go
  server/cmd/server/router.go
  server/cmd/server/notification_listeners.go
  server/pkg/db/queries/inbox.sql
)

if rg -n -i --glob '!*.test.*' --glob '!*_test.go' "$forbidden" "${implementation[@]}"; then
  echo "Round simplification validation failed: legacy execution behavior remains." >&2
  exit 1
fi

required_routes=(
  'r.Get("/", cerebroRoundsHandler.List)'
  'r.Post("/", cerebroRoundsHandler.Create)'
  'r.Get("/status", cerebroRoundsHandler.Status)'
  'r.Post("/start", cerebroRoundsHandler.Start)'
  'r.Post("/members", cerebroRoundsHandler.AddMember)'
  'r.Delete("/members/{issueId}", cerebroRoundsHandler.RemoveMember)'
)
for route in "${required_routes[@]}"; do
  rg -F -q "$route" server/cmd/server/router.go || {
    echo "Round simplification validation failed: missing route $route" >&2
    exit 1
  }
done

rg -q 'CREATE TABLE (IF NOT EXISTS )?cerebro_round_cycle ' server/migrations/9136_cerebro_round_simplification.up.sql
rg -q 'CREATE TABLE (IF NOT EXISTS )?cerebro_round_cycle_item ' server/migrations/9136_cerebro_round_simplification.up.sql
rg -q 'type RoundView = "ready" \| "handled" \| "all"' packages/cerebro-rounds/rounds-block.tsx

echo "Round simplification validation passed."

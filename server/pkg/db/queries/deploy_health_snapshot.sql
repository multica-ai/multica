-- Phase 5 Ship Hub — post-deploy live health snapshots.
--
-- Append-only. The 5-minute health monitor goroutine (see
-- cmd/server/ship_hub_health_monitor.go) reads the latest snapshot for
-- each in-window deploy, computes the next one, and writes a fresh row.
-- The card UI reads only the latest row per deploy.

-- name: InsertDeployHealthSnapshot :one
INSERT INTO deploy_health_snapshot (
    workspace_id, deploy_id,
    error_rate_baseline, error_rate_current,
    p99_latency_baseline_ms, p99_latency_current_ms,
    inbox_issues_since, agent_failure_rate_delta
) VALUES (
    $1, $2,
    sqlc.narg('error_rate_baseline'), sqlc.narg('error_rate_current'),
    sqlc.narg('p99_latency_baseline_ms'), sqlc.narg('p99_latency_current_ms'),
    $3, sqlc.narg('agent_failure_rate_delta')
)
RETURNING *;

-- name: GetLatestDeployHealthSnapshot :one
SELECT * FROM deploy_health_snapshot
WHERE deploy_id = $1
ORDER BY snapshot_at DESC
LIMIT 1;

-- name: ListDeployHealthSnapshots :many
SELECT * FROM deploy_health_snapshot
WHERE deploy_id = $1
ORDER BY snapshot_at DESC
LIMIT sqlc.arg('limit');

-- name: ListRecentSucceededDeploys :many
-- Drives the health monitor. Returns one row per deploy that succeeded
-- in the last 24h so the goroutine can iterate and refresh snapshots.
-- We exclude deploys without a completed_at (still in-flight) and the
-- ones older than 24h (no point monitoring a stable deploy forever).
SELECT d.*
FROM deploy d
WHERE d.status = 'succeeded'
  AND d.completed_at IS NOT NULL
  AND d.completed_at > NOW() - INTERVAL '24 hours'
ORDER BY d.completed_at DESC;

-- name: ListRecentInboxOpensSinceForWorkspace :one
-- Backfill helper for the inbox_issues_since field. Counts inbox rows
-- created in this workspace since the deploy's completed_at. Cheap
-- because inbox writes are infrequent and the index covers
-- (workspace_id, created_at).
SELECT COUNT(*)::int AS opens_since
FROM inbox_item
WHERE workspace_id = $1
  AND created_at >= $2;

-- name: AgentTaskFailureRateInWindow :one
-- Returns (failed_count, total_count) for tasks that were in_progress
-- or completed in a time window. The Δ is computed in Go because
-- pgnumeric arithmetic on small denominators introduces floating
-- weirdness — the SQL just returns the integers.
SELECT
    SUM(CASE WHEN atq.status = 'failed' THEN 1 ELSE 0 END)::int AS failed,
    COUNT(*)::int AS total
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = $1
  AND atq.completed_at IS NOT NULL
  AND atq.completed_at >= $2
  AND atq.completed_at <  $3;

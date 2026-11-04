-- The office pulse (spec 4.8) and the system health readings (spec 4.9). Every
-- query filters by workspace_id unless it reads the scheduler's own log.

-- name: CountPendingProposalsForPulse :one
SELECT count(*)::int AS pending, min(created_at)::timestamptz AS oldest_created_at
FROM proposal
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND state = 'pending';

-- name: CountOfflineRuntimes :one
SELECT count(*)::int FROM agent_runtime
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND status = 'offline';

-- name: CountFailedBriefDeliveries :one
-- Brief deliveries to chat apps that failed and have not been sent since.
SELECT count(*)::int FROM brief_delivery
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND state = 'failed'
  AND created_at >= sqlc.arg('since')::timestamptz;

-- name: ListSchedulerJobRuns :many
-- The scheduler's execution log, summarised per job for the job status registry.
-- last_success_at is the newest finished success; stuck counts runs still
-- RUNNING past their stale_after; last_failed_at is the newest failure.
SELECT job_name,
       max(finished_at) FILTER (WHERE status = 'SUCCESS')::timestamptz AS last_success_at,
       max(finished_at) FILTER (WHERE status = 'FAILED')::timestamptz AS last_failed_at,
       count(*) FILTER (WHERE status = 'RUNNING' AND stale_after < sqlc.arg('now')::timestamptz)::int AS stuck
FROM sys_cron_executions
WHERE job_name = ANY(sqlc.arg('job_names')::text[])
  AND plan_time >= sqlc.arg('since')::timestamptz
GROUP BY job_name;

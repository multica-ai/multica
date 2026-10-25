-- SE-37663: Agent Fleet health aggregates for GET /api/se/health.
-- Read-only views over agent_runtime and agent_task_queue; no new tables,
-- no new migrations. Failure classes follow the agent_error.* namespace
-- already written by the task pipeline into agent_task_queue.failure_reason.

-- name: ListFleetRuntimes :many
SELECT r.name, r.provider, r.status, r.last_seen_at
FROM agent_runtime r
WHERE r.workspace_id = $1
  AND r.last_seen_at > now() - interval '3 days'
ORDER BY r.last_seen_at DESC NULLS LAST;

-- name: GetFleetAgentRunStats :many
SELECT a.id AS agent_id, a.name AS agent_name,
  COUNT(*) FILTER (WHERE q.status = 'completed') AS completed,
  COUNT(*) FILTER (WHERE q.status = 'failed') AS failed,
  COUNT(*) FILTER (WHERE q.status = 'failed' AND q.failure_reason = 'agent_error.provider_quota_limit') AS quota_failed,
  COUNT(*) FILTER (WHERE q.status = 'failed' AND q.failure_reason = 'agent_error.provider_auth_or_access') AS auth_failed,
  to_char(MIN(q.created_at) FILTER (WHERE q.status = 'failed') AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"') AS first_failure_at,
  to_char(MAX(q.created_at) FILTER (WHERE q.status = 'failed') AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"') AS last_failure_at
FROM agent a
JOIN agent_task_queue q ON q.agent_id = a.id
WHERE a.workspace_id = $1
  AND q.created_at > now() - interval '48 hours'
  AND q.status IN ('completed', 'failed')
GROUP BY a.id, a.name
HAVING COUNT(*) FILTER (WHERE q.status = 'failed') > 0
ORDER BY failed DESC, a.name;

-- name: GetFleetAgentLastFailures :many
SELECT DISTINCT ON (q.agent_id)
  q.agent_id, q.created_at, q.failure_reason,
  left(COALESCE(q.error, q.failure_reason, ''), 240) AS error_excerpt
FROM agent_task_queue q
JOIN agent a ON a.id = q.agent_id
WHERE a.workspace_id = $1
  AND q.status = 'failed'
  AND q.created_at > now() - interval '48 hours'
ORDER BY q.agent_id, q.created_at DESC;

-- name: GetFleetFailureStreaks :one
SELECT
  EXISTS (
    SELECT 1
    FROM agent_task_queue q
    JOIN agent a ON a.id = q.agent_id
    WHERE a.workspace_id = $1
      AND q.status = 'failed'
      AND q.failure_reason = 'agent_error.provider_quota_limit'
      AND q.created_at > now() - interval '3 hours'
  ) AS quota_streak_active,
  EXISTS (
    SELECT 1
    FROM agent_task_queue q
    JOIN agent a ON a.id = q.agent_id
    WHERE a.workspace_id = $1
      AND q.status = 'failed'
      AND q.failure_reason = 'agent_error.provider_auth_or_access'
      AND q.created_at > now() - interval '3 hours'
  ) AS auth_streak_active;

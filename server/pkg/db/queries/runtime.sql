-- name: ListAgentRuntimes :many
SELECT * FROM agent_runtime
WHERE workspace_id = ?
ORDER BY created_at ASC;

-- name: GetAgentRuntime :one
SELECT * FROM agent_runtime
WHERE id = ?;

-- name: GetAgentRuntimeForWorkspace :one
SELECT * FROM agent_runtime
WHERE id = ? AND workspace_id = ?;

-- name: UpsertAgentRuntime :one
INSERT INTO agent_runtime (
    id,
    workspace_id,
    daemon_id,
    name,
    runtime_mode,
    provider,
    status,
    device_info,
    metadata,
    last_seen_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
ON CONFLICT (workspace_id, daemon_id, provider)
DO UPDATE SET
    name = EXCLUDED.name,
    runtime_mode = EXCLUDED.runtime_mode,
    status = EXCLUDED.status,
    device_info = EXCLUDED.device_info,
    metadata = EXCLUDED.metadata,
    last_seen_at = datetime('now'),
    updated_at = datetime('now')
RETURNING *;

-- name: UpdateAgentRuntimeHeartbeat :one
UPDATE agent_runtime
SET status = 'online', last_seen_at = datetime('now'), updated_at = datetime('now')
WHERE id = ?
RETURNING *;

-- name: SetAgentRuntimeOffline :exec
UPDATE agent_runtime
SET status = 'offline', updated_at = datetime('now')
WHERE id = ?;

-- name: MarkStaleRuntimesOffline :many
UPDATE agent_runtime
SET status = 'offline', updated_at = datetime('now')
WHERE status = 'online'
  AND last_seen_at < datetime('now', '-' || CAST(sqlc.arg(stale_seconds) AS TEXT) || ' seconds')
RETURNING id, workspace_id;

-- name: FailTasksForOfflineRuntimes :many
UPDATE agent_task_queue
SET status = 'failed', completed_at = datetime('now'), error = 'runtime went offline'
WHERE status IN ('dispatched', 'running')
  AND runtime_id IN (
    SELECT id FROM agent_runtime WHERE status = 'offline'
  )
RETURNING id, agent_id, issue_id;

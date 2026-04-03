-- name: ListAgents :many
SELECT * FROM agent
WHERE workspace_id = ? AND archived_at IS NULL
ORDER BY created_at ASC;

-- name: ListAllAgents :many
SELECT * FROM agent
WHERE workspace_id = ?
ORDER BY created_at ASC;

-- name: GetAgent :one
SELECT * FROM agent
WHERE id = ?;

-- name: GetAgentInWorkspace :one
SELECT * FROM agent
WHERE id = ? AND workspace_id = ?;

-- name: CreateAgent :one
INSERT INTO agent (
    id, workspace_id, name, description, avatar_url, runtime_mode,
    runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id,
    tools, triggers, instructions
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateAgent :one
UPDATE agent SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    avatar_url = COALESCE(sqlc.narg('avatar_url'), avatar_url),
    runtime_config = COALESCE(sqlc.narg('runtime_config'), runtime_config),
    runtime_mode = COALESCE(sqlc.narg('runtime_mode'), runtime_mode),
    runtime_id = COALESCE(sqlc.narg('runtime_id'), runtime_id),
    visibility = COALESCE(sqlc.narg('visibility'), visibility),
    status = COALESCE(sqlc.narg('status'), status),
    max_concurrent_tasks = COALESCE(sqlc.narg('max_concurrent_tasks'), max_concurrent_tasks),
    tools = COALESCE(sqlc.narg('tools'), tools),
    triggers = COALESCE(sqlc.narg('triggers'), triggers),
    instructions = COALESCE(sqlc.narg('instructions'), instructions),
    updated_at = datetime('now')
WHERE id = ?
RETURNING *;

-- name: ArchiveAgent :one
UPDATE agent SET archived_at = datetime('now'), archived_by = ?, updated_at = datetime('now')
WHERE id = ?
RETURNING *;

-- name: RestoreAgent :one
UPDATE agent SET archived_at = NULL, archived_by = NULL, updated_at = datetime('now')
WHERE id = ?
RETURNING *;

-- name: ListAgentTasks :many
SELECT * FROM agent_task_queue
WHERE agent_id = ?
ORDER BY created_at DESC;

-- name: CreateAgentTask :one
INSERT INTO agent_task_queue (id, agent_id, runtime_id, issue_id, status, priority, trigger_comment_id)
VALUES (?, ?, ?, ?, 'queued', ?, sqlc.narg(trigger_comment_id))
RETURNING *;

-- name: CancelAgentTasksByIssue :exec
UPDATE agent_task_queue
SET status = 'cancelled'
WHERE issue_id = ? AND status IN ('queued', 'dispatched', 'running');

-- name: CancelAgentTasksByAgent :exec
UPDATE agent_task_queue
SET status = 'cancelled'
WHERE agent_id = ? AND status IN ('queued', 'dispatched', 'running');

-- name: GetAgentTask :one
SELECT * FROM agent_task_queue
WHERE id = ?;

-- name: ClaimAgentTask :one
-- Claims the next queued task for an agent, enforcing per-issue serialization.
-- SQLite single-writer model provides implicit serialization (no FOR UPDATE needed).
UPDATE agent_task_queue
SET status = 'dispatched', dispatched_at = datetime('now')
WHERE id = (
    SELECT atq.id FROM agent_task_queue atq
    WHERE atq.agent_id = ? AND atq.status = 'queued'
      AND NOT EXISTS (
          SELECT 1 FROM agent_task_queue active
          WHERE active.issue_id = atq.issue_id
            AND active.status IN ('dispatched', 'running')
      )
    ORDER BY atq.priority DESC, atq.created_at ASC
    LIMIT 1
)
RETURNING *;

-- name: StartAgentTask :one
UPDATE agent_task_queue
SET status = 'running', started_at = datetime('now')
WHERE id = ? AND status = 'dispatched'
RETURNING *;

-- name: CompleteAgentTask :one
UPDATE agent_task_queue
SET status = 'completed', completed_at = datetime('now'), result = ?, session_id = ?, work_dir = ?
WHERE id = ? AND status = 'running'
RETURNING *;

-- name: GetLastTaskSession :one
SELECT session_id, work_dir FROM agent_task_queue
WHERE agent_id = ? AND issue_id = ? AND status = 'completed' AND session_id IS NOT NULL
ORDER BY completed_at DESC
LIMIT 1;

-- name: FailAgentTask :one
UPDATE agent_task_queue
SET status = 'failed', completed_at = datetime('now'), error = ?
WHERE id = ? AND status IN ('dispatched', 'running')
RETURNING *;

-- name: FailStaleTasks :many
-- Fails tasks stuck in dispatched/running beyond the given thresholds.
UPDATE agent_task_queue
SET status = 'failed', completed_at = datetime('now'), error = 'task timed out'
WHERE (status = 'dispatched' AND dispatched_at < datetime('now', '-' || CAST(sqlc.arg(dispatch_timeout_secs) AS TEXT) || ' seconds'))
   OR (status = 'running' AND started_at < datetime('now', '-' || CAST(sqlc.arg(running_timeout_secs) AS TEXT) || ' seconds'))
RETURNING id, agent_id, issue_id;

-- name: CancelAgentTask :one
UPDATE agent_task_queue
SET status = 'cancelled', completed_at = datetime('now')
WHERE id = ? AND status IN ('queued', 'dispatched', 'running')
RETURNING *;

-- name: CountRunningTasks :one
SELECT count(*) FROM agent_task_queue
WHERE agent_id = ? AND status IN ('dispatched', 'running');

-- name: HasActiveTaskForIssue :one
SELECT count(*) > 0 AS has_active FROM agent_task_queue
WHERE issue_id = ? AND status IN ('queued', 'dispatched', 'running');

-- name: HasPendingTaskForIssue :one
SELECT count(*) > 0 AS has_pending FROM agent_task_queue
WHERE issue_id = ? AND status IN ('queued', 'dispatched');

-- name: HasPendingTaskForIssueAndAgent :one
SELECT count(*) > 0 AS has_pending FROM agent_task_queue
WHERE issue_id = ? AND agent_id = ? AND status IN ('queued', 'dispatched');

-- name: ListPendingTasksByRuntime :many
SELECT * FROM agent_task_queue
WHERE runtime_id = ? AND status IN ('queued', 'dispatched')
ORDER BY priority DESC, created_at ASC;

-- name: ListActiveTasksByIssue :many
SELECT * FROM agent_task_queue
WHERE issue_id = ? AND status IN ('dispatched', 'running')
ORDER BY created_at DESC;

-- name: ListTasksByIssue :many
SELECT * FROM agent_task_queue
WHERE issue_id = ?
ORDER BY created_at DESC;

-- name: UpdateAgentStatus :one
UPDATE agent SET status = ?, updated_at = datetime('now')
WHERE id = ?
RETURNING *;

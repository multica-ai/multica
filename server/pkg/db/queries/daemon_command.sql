-- name: GetLatestOpenIdeTaskForIssue :one
SELECT
    atq.id AS task_id,
    atq.issue_id,
    i.parent_issue_id,
    i.project_id,
    atq.work_dir,
    a.name AS agent_name,
    atq.runtime_id,
    ar.workspace_id,
    ar.daemon_id,
    ar.runtime_mode,
    ar.status AS runtime_status,
    ar.owner_id
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
JOIN agent_runtime ar ON ar.id = atq.runtime_id
JOIN issue i ON i.id = atq.issue_id
WHERE atq.issue_id = @issue_id
  AND i.workspace_id = @workspace_id
  AND ar.workspace_id = @workspace_id
  AND ar.runtime_mode = 'local'
  AND atq.work_dir IS NOT NULL
  AND atq.work_dir <> ''
ORDER BY atq.created_at DESC
LIMIT 1;

-- name: GetOpenIdeTaskByID :one
SELECT
    atq.id AS task_id,
    atq.issue_id,
    i.parent_issue_id,
    i.project_id,
    atq.work_dir,
    a.name AS agent_name,
    atq.runtime_id,
    ar.workspace_id,
    ar.daemon_id,
    ar.runtime_mode,
    ar.status AS runtime_status,
    ar.owner_id
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
JOIN agent_runtime ar ON ar.id = atq.runtime_id
JOIN issue i ON i.id = atq.issue_id
WHERE atq.id = @task_id
  AND i.workspace_id = @workspace_id
  AND ar.workspace_id = @workspace_id
  AND ar.runtime_mode = 'local'
  AND atq.work_dir IS NOT NULL
  AND atq.work_dir <> ''
LIMIT 1;

-- name: CreateDaemonCommand :one
INSERT INTO daemon_command (
    workspace_id, daemon_id, runtime_id, requester_user_id,
    issue_id, task_id, command_type, payload
) VALUES (
    @workspace_id, @daemon_id, @runtime_id, @requester_user_id,
    @issue_id, @task_id, @command_type, @payload
)
RETURNING *;

-- name: GetOpenIdeCommandForRequester :one
SELECT *
FROM daemon_command
WHERE id = @id
  AND workspace_id = @workspace_id
  AND issue_id = @issue_id
  AND requester_user_id = @requester_user_id
  AND command_type = 'open_intellij'
LIMIT 1;

-- name: ClaimDaemonCommands :many
UPDATE daemon_command dc
SET status = 'claimed', claimed_at = now()
WHERE dc.id IN (
    SELECT id
    FROM daemon_command
    WHERE daemon_command.daemon_id = @daemon_id
      AND daemon_command.status = 'queued'
    ORDER BY daemon_command.created_at ASC
    LIMIT @limit_count::int
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: ClaimOwnedDaemonCommands :many
UPDATE daemon_command dc
SET status = 'claimed', claimed_at = now()
WHERE dc.id IN (
    SELECT daemon_command.id
    FROM daemon_command
    JOIN agent_runtime ar ON ar.id = daemon_command.runtime_id
    WHERE daemon_command.daemon_id = @daemon_id
      AND daemon_command.status = 'queued'
      AND ar.daemon_id = @daemon_id
      AND ar.runtime_mode = 'local'
      AND ar.owner_id = @owner_id
    ORDER BY daemon_command.created_at ASC
    LIMIT @limit_count::int
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: CompleteDaemonCommand :one
UPDATE daemon_command
SET status = @status, completed_at = now(), error = sqlc.narg('error')
WHERE id = @id
  AND daemon_command.daemon_id = @daemon_id
  AND daemon_command.status = 'claimed'
RETURNING *;

-- name: CompleteOwnedDaemonCommand :one
UPDATE daemon_command
SET status = @status, completed_at = now(), error = sqlc.narg('error')
WHERE daemon_command.id = @id
  AND daemon_command.daemon_id = @daemon_id
  AND daemon_command.status = 'claimed'
  AND EXISTS (
      SELECT 1
      FROM agent_runtime ar
      WHERE ar.id = daemon_command.runtime_id
        AND ar.daemon_id = @daemon_id
        AND ar.runtime_mode = 'local'
        AND ar.owner_id = @owner_id
  )
RETURNING *;

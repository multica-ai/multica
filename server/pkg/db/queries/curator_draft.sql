-- name: CreateCuratorDraftTask :one
INSERT INTO curator_draft_task (
    workspace_id, runtime_id, draft_kind, input_data, created_by
) VALUES (
    sqlc.arg('workspace_id'), sqlc.arg('runtime_id'), sqlc.arg('draft_kind'),
    sqlc.arg('input_data'), sqlc.arg('created_by')
)
RETURNING *;

-- name: ClaimNextCuratorDraftTask :one
WITH next_task AS (
    SELECT id
    FROM curator_draft_task
    WHERE curator_draft_task.runtime_id = sqlc.arg('runtime_id')
      AND curator_draft_task.workspace_id = sqlc.arg('workspace_id')
      AND curator_draft_task.status = 'queued'
    ORDER BY curator_draft_task.created_at ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
UPDATE curator_draft_task
SET status = 'running', updated_at = now()
FROM next_task
WHERE curator_draft_task.id = next_task.id
RETURNING curator_draft_task.*;

-- name: CompleteCuratorDraftTask :one
UPDATE curator_draft_task
SET status = 'completed', result = sqlc.arg('result'), updated_at = now()
WHERE id = sqlc.arg('id')
  AND runtime_id = sqlc.arg('runtime_id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND status = 'running'
RETURNING *;

-- name: FailCuratorDraftTask :one
UPDATE curator_draft_task
SET status = 'failed', error = sqlc.arg('error'), updated_at = now()
WHERE id = sqlc.arg('id')
  AND runtime_id = sqlc.arg('runtime_id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND status = 'running'
RETURNING *;

-- name: GetCuratorDraftTask :one
SELECT *
FROM curator_draft_task
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id');

-- name: ListOnlineDaemonRuntimes :many
SELECT *
FROM agent_runtime
WHERE workspace_id = sqlc.arg('workspace_id')
  AND runtime_mode = 'local'
  AND status = 'online'
ORDER BY last_seen_at DESC;

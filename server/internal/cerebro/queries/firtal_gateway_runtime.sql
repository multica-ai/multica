-- name: ListFirtalGatewayWorkspaceIDs :many
SELECT id FROM workspace
ORDER BY created_at ASC;

-- name: ListFirtalGatewayWorkspaceIDsByID :many
SELECT id FROM workspace
WHERE id = ANY(@ids::uuid[])
ORDER BY created_at ASC;

-- name: ListFirtalGatewayRuntimes :many
SELECT * FROM agent_runtime
WHERE provider = @provider
  AND daemon_id = @daemon_id
ORDER BY created_at ASC;

-- name: ClaimFirtalGatewayChatTask :one
WITH next_task AS (
    SELECT atq.id
    FROM agent_task_queue atq
    JOIN agent a ON a.id = atq.agent_id
    WHERE atq.runtime_id = @runtime_id
      AND atq.status = 'queued'
      AND atq.chat_session_id IS NOT NULL
      AND (
          SELECT count(*)
          FROM agent_task_queue running
          WHERE running.agent_id = atq.agent_id
            AND running.status IN ('dispatched', 'running')
      ) < a.max_concurrent_tasks
      AND NOT EXISTS (
          SELECT 1
          FROM agent_task_queue active
          WHERE active.agent_id = atq.agent_id
            AND active.status IN ('dispatched', 'running')
            AND active.chat_session_id = atq.chat_session_id
      )
    ORDER BY atq.priority DESC, atq.created_at ASC
    LIMIT 1
    FOR UPDATE OF atq SKIP LOCKED
)
UPDATE agent_task_queue
SET status = 'dispatched', dispatched_at = now()
WHERE id = (SELECT id FROM next_task)
RETURNING *;

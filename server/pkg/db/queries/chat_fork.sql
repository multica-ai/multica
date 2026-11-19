-- name: ReserveChatForkSnapshot :one
-- Call after LockChatSessionForEnqueue in the same transaction.
UPDATE chat_session cs
SET fork_snapshot_task_id = sqlc.arg(task_id)
WHERE cs.id = sqlc.arg(source_chat_id)
  AND cs.workspace_id = sqlc.arg(workspace_id)
  AND cs.creator_id = sqlc.arg(creator_id)
  AND cs.status = 'active'
  AND NULLIF(cs.session_id, '') IS NOT NULL
  AND cs.runtime_id IS NOT NULL
  AND cs.fork_initialization_task_id IS NULL
  AND cs.fork_snapshot_task_id IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM agent_task_queue t
      WHERE t.chat_session_id = cs.id
        AND t.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
  )
RETURNING cs.*;

-- name: CreatePendingChatFork :one
INSERT INTO chat_session (
    id, workspace_id, agent_id, creator_id, title, runtime_id, project_id,
    forked_from_chat_id, fork_initialization_task_id
)
SELECT sqlc.arg(destination_chat_id), cs.workspace_id, cs.agent_id, cs.creator_id, cs.title,
       cs.runtime_id, cs.project_id, cs.id, cs.fork_snapshot_task_id
FROM chat_session cs
WHERE cs.id = sqlc.arg(source_chat_id)
  AND cs.fork_snapshot_task_id = sqlc.arg(task_id)
RETURNING *;

-- name: GetPendingChatForkSource :one
SELECT source.*
FROM chat_session destination
JOIN chat_session source ON source.id = destination.forked_from_chat_id
WHERE destination.id = sqlc.arg(destination_chat_id)
  AND destination.fork_initialization_task_id = sqlc.arg(task_id)
  AND source.fork_snapshot_task_id = destination.fork_initialization_task_id
  AND source.workspace_id = destination.workspace_id
  AND source.creator_id = destination.creator_id
  AND source.agent_id = destination.agent_id
  AND source.runtime_id = destination.runtime_id
  AND source.status = 'active';

-- name: CompletePendingChatFork :one
UPDATE chat_session destination
SET session_id = sqlc.arg(session_id),
    fork_initialization_task_id = NULL,
    explicitly_created_at = now(),
    updated_at = now()
WHERE destination.id = sqlc.arg(destination_chat_id)
  AND destination.fork_initialization_task_id = sqlc.arg(task_id)
  AND NULLIF(sqlc.arg(session_id), '') IS NOT NULL
  AND EXISTS (
      SELECT 1 FROM chat_session source
      WHERE source.id = destination.forked_from_chat_id
        AND source.fork_snapshot_task_id = destination.fork_initialization_task_id
        AND source.workspace_id = destination.workspace_id
        AND source.runtime_id = destination.runtime_id
        AND source.status = 'active'
  )
RETURNING destination.*;

-- name: ReleaseChatForkSnapshot :execrows
UPDATE chat_session
SET fork_snapshot_task_id = NULL
WHERE id = sqlc.arg(source_chat_id)
  AND fork_snapshot_task_id = sqlc.arg(task_id);

-- name: DeletePendingChatFork :execrows
DELETE FROM chat_session
WHERE id = sqlc.arg(destination_chat_id)
  AND fork_initialization_task_id = sqlc.arg(task_id);

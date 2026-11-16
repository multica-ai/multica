-- name: LockRunningChatTaskForSupplement :one
SELECT t.* FROM agent_task_queue t
JOIN task_supplement_capability cap ON cap.task_id = t.id
WHERE t.chat_session_id = @chat_session_id AND t.status = 'running'
  AND t.regenerate_quick_actions_for IS NULL
  AND cap.capability = 'task-supplement-v1'
  AND NOT EXISTS (SELECT 1 FROM channel_chat_session_binding b WHERE b.chat_session_id = t.chat_session_id)
ORDER BY t.created_at, t.id
LIMIT 1
FOR UPDATE OF t;

-- name: CreateChatTaskSupplement :exec
INSERT INTO chat_task_supplement (task_id, message_id, chat_session_id, workspace_id, author_id, status)
VALUES (@task_id, @message_id, @chat_session_id, @workspace_id, @author_id, 'pending');

-- name: ClaimNextChatTaskSupplement :one
WITH next AS MATERIALIZED (
    SELECT s.message_id FROM chat_task_supplement s
    JOIN agent_task_queue t ON t.id = s.task_id
    WHERE s.task_id = @task_id AND s.status = 'pending' AND t.status = 'running'
    ORDER BY s.created_at, s.message_id
    LIMIT 1 FOR UPDATE OF s SKIP LOCKED
), claimed AS (
    UPDATE chat_task_supplement s SET status = 'delivering'
    FROM next WHERE s.message_id = next.message_id RETURNING s.*
)
SELECT claimed.message_id, m.content, COALESCE(NULLIF(btrim(u.name), ''), 'a user')::text AS author_name
FROM claimed JOIN chat_message m ON m.id = claimed.message_id
LEFT JOIN "user" u ON u.id = claimed.author_id;

-- name: AckChatTaskSupplement :one
UPDATE chat_task_supplement SET status = 'delivered'
WHERE task_id = @task_id AND message_id = @message_id AND status IN ('delivering', 'delivered')
RETURNING *;

-- name: LockUndeliveredChatTaskSupplements :many
SELECT s.* FROM chat_task_supplement s
WHERE s.task_id = @task_id AND s.status IN ('pending', 'delivering')
ORDER BY s.created_at, s.message_id FOR UPDATE;

-- name: RequeueChatTaskSupplement :exec
WITH moved AS (
    UPDATE chat_task_supplement SET status = 'requeued'
    WHERE message_id = @message_id AND status IN ('pending', 'delivering')
    RETURNING message_id
)
UPDATE chat_message SET task_id = @next_task_id
WHERE id IN (SELECT message_id FROM moved);

-- name: RetainChatTaskSupplementsForRetry :exec
-- Retire delivery into the failed attempt. The message stays in the root
-- input batch and reaches the retry as ordinary input, not another steer.
UPDATE chat_task_supplement SET status = 'withdrawn'
WHERE task_id = $1 AND status IN ('pending', 'delivering');

-- name: DeleteChatTaskSupplementsBySession :exec
DELETE FROM chat_task_supplement WHERE chat_session_id = $1;

-- name: HasChatTaskAssistantOutcome :one
SELECT EXISTS (SELECT 1 FROM chat_message WHERE task_id = $1 AND role = 'assistant');

-- name: ListChatGuidanceInProviderSession :many
SELECT s.message_id FROM chat_task_supplement s
JOIN agent_task_queue t ON t.id = s.task_id
WHERE s.workspace_id = @workspace_id AND s.message_id = ANY(@message_ids::uuid[])
  AND s.status = 'delivered'
  AND t.session_id = @session_id AND t.runtime_id = @runtime_id;

-- name: DeleteChatTaskSupplementsByWorkspace :exec
DELETE FROM chat_task_supplement WHERE workspace_id = $1;

-- name: DeleteChatSupplementCapabilitiesBySession :exec
DELETE FROM task_supplement_capability
WHERE task_id IN (SELECT id FROM agent_task_queue WHERE chat_session_id = $1);

-- name: WithdrawChatTaskSupplementsByRuntime :exec
UPDATE chat_task_supplement SET status = 'withdrawn'
WHERE task_id IN (SELECT id FROM agent_task_queue WHERE runtime_id = $1)
  AND status IN ('pending', 'delivering');

-- name: WithdrawChatTaskSupplementsByTasks :exec
UPDATE chat_task_supplement SET status = 'withdrawn'
WHERE task_id = ANY(@task_ids::uuid[]) AND status IN ('pending', 'delivering');

-- name: WithdrawChatTaskSupplementsByAgent :exec
UPDATE chat_task_supplement SET status = 'withdrawn'
WHERE task_id IN (SELECT id FROM agent_task_queue WHERE agent_id = $1)
  AND status IN ('pending', 'delivering');

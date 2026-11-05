-- name: CreateChatSupplement :one
-- The session lock serializes with archive/delete; the exact task lock with
-- completion. A losing request creates neither a message nor a receipt.
WITH session AS MATERIALIZED (
    SELECT cs.id FROM chat_session cs
    WHERE cs.id = @chat_session_id AND cs.workspace_id = @workspace_id
      AND cs.creator_id = @author_id AND cs.status = 'active'
      AND NOT EXISTS (SELECT 1 FROM channel_chat_session_binding b WHERE b.chat_session_id = cs.id)
    FOR UPDATE OF cs
), task AS MATERIALIZED (
    SELECT t.id FROM agent_task_queue t
    JOIN session cs ON cs.id = t.chat_session_id
    JOIN task_supplement_capability cap ON cap.task_id = t.id
    WHERE t.id = @task_id AND t.status = 'running' AND t.issue_id IS NULL
      AND cap.capability = 'task-supplement-v1'
    FOR UPDATE OF t
), message AS (
    INSERT INTO chat_message (chat_session_id, task_id, role, content)
    SELECT @chat_session_id, task.id, 'user', @content FROM task
    RETURNING *
), receipt AS (
    INSERT INTO chat_supplement (message_id, task_id, chat_session_id, workspace_id, author_id, client_request_id, status)
    SELECT message.id, message.task_id, message.chat_session_id, @workspace_id, @author_id, @client_request_id, 'pending'
    FROM message
    RETURNING message_id
), touched AS (
    UPDATE chat_session SET updated_at = now() WHERE id IN (SELECT chat_session_id FROM message)
)
SELECT message.* FROM message JOIN receipt ON receipt.message_id = message.id;

-- name: GetChatSupplementByRequest :one
SELECT m.* FROM chat_message m JOIN chat_supplement s ON s.message_id = m.id
WHERE s.task_id = @task_id AND s.author_id = @author_id AND s.client_request_id = @client_request_id
  AND s.chat_session_id = @chat_session_id AND s.workspace_id = @workspace_id;

-- name: ListChatSupplements :many
SELECT * FROM chat_supplement WHERE workspace_id = @workspace_id AND message_id = ANY(@message_ids::uuid[]);

-- name: ClaimNextChatSupplement :one
WITH task AS MATERIALIZED (
    SELECT agent_task_queue.id FROM agent_task_queue WHERE agent_task_queue.id = @task_id AND status = 'running' FOR UPDATE
), next AS MATERIALIZED (
    SELECT s.message_id FROM chat_supplement s JOIN task ON task.id = s.task_id
    WHERE s.status = 'pending'
    ORDER BY s.created_at, s.message_id LIMIT 1 FOR UPDATE OF s
), claimed AS (
    UPDATE chat_supplement SET status = 'delivering'
    WHERE message_id IN (SELECT message_id FROM next)
    RETURNING *
)
SELECT claimed.message_id, m.content, COALESCE(u.name, 'a user')::text AS author_name
FROM claimed JOIN chat_message m ON m.id = claimed.message_id
LEFT JOIN "user" u ON u.id = claimed.author_id;

-- name: AckChatSupplement :one
-- A late positive engine receipt may resolve an uncertain terminal settlement.
WITH task AS MATERIALIZED (
    SELECT agent_task_queue.id FROM agent_task_queue WHERE agent_task_queue.id = @task_id FOR UPDATE
)
UPDATE chat_supplement s
SET status = @status, failure_reason = sqlc.narg(failure_reason),
    delivered_at = CASE WHEN @status::text = 'delivered' THEN COALESCE(delivered_at, now()) ELSE delivered_at END
FROM task WHERE s.task_id = task.id AND s.message_id = @message_id
  AND (s.status = 'delivering' OR (s.status IN ('unknown', 'delivered') AND @status::text = 'delivered'))
RETURNING s.*;

-- name: SettleTerminalChatSupplements :exec
UPDATE chat_supplement s
SET status = CASE WHEN s.status = 'pending' THEN 'failed' ELSE 'unknown' END,
    failure_reason = 'turn_ended'
FROM agent_task_queue t
WHERE t.id = s.task_id AND t.id = ANY(@task_ids::uuid[])
  AND t.status IN ('completed', 'failed', 'cancelled') AND s.status IN ('pending', 'delivering');

-- name: DeleteChatSupplementsBySession :exec
DELETE FROM chat_supplement WHERE chat_session_id = $1;

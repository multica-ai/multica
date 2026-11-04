-- Replies the Secretary writes into a chat session from a later run (`multica chat reply`).

-- name: CountRecentChatAssistantMessages :one
-- Assistant messages written to one session since a cut-off. Backs the per-session
-- cap on chat replies; every assistant row counts, so the cap bounds the whole session.
SELECT count(*)::bigint FROM chat_message
WHERE chat_session_id = sqlc.arg('chat_session_id')::uuid
  AND role = 'assistant'
  AND created_at > sqlc.arg('since')::timestamptz;

-- name: GetLatestChannelDeliveryTaskInSession :one
-- The most recent chat run in this session that carries a channel delivery
-- snapshot. A Secretary reply from a later run takes its route from here, so an
-- external channel bound to the session hears the reply.
SELECT t.id
FROM agent_task_queue t
JOIN channel_task_delivery d ON d.task_id = t.id
WHERE t.chat_session_id = @chat_session_id AND t.id <> @run_id
ORDER BY t.created_at DESC, t.id DESC
LIMIT 1;

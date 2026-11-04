-- Replies the Secretary writes into a chat session from a later run (`multica chat reply`).

-- name: CountRecentChatAssistantMessages :one
-- Assistant messages written to one session since a cut-off. Backs the per-session
-- cap on chat replies; every assistant row counts, so the cap bounds the whole session.
SELECT count(*)::bigint FROM chat_message
WHERE chat_session_id = sqlc.arg('chat_session_id')::uuid
  AND role = 'assistant'
  AND created_at > sqlc.arg('since')::timestamptz;

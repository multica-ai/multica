-- Event-driven unread tracking for chat sessions.
--
-- Semantics: unread_since is the timestamp of the first unread assistant
-- message. It stays NULL while the session has no unread. It's SET when
-- an assistant reply lands and the column was NULL. It's RESET to NULL
-- when the user marks the session as read. Existing rows start as NULL,
-- meaning "no unread to track" — historic chats are not mass-flagged.
ALTER TABLE chat_session ADD COLUMN unread_since TIMESTAMPTZ;

-- GetPendingChatTask runs on every session open / switch and filters by
-- chat_session_id + in-flight status + orders by created_at. A partial
-- index on the in-flight subset keeps that query cheap as the queue grows.
CREATE INDEX IF NOT EXISTS idx_agent_task_queue_chat_pending
  ON agent_task_queue (chat_session_id, created_at DESC)
  WHERE chat_session_id IS NOT NULL
    AND status IN ('queued', 'dispatched', 'running');

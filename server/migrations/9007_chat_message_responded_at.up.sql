-- Track which user chat messages have been answered by an assistant turn.
-- Replaces the time-based "newer than last assistant" filter, which silently
-- dropped messages sent mid-stream: a user follow-up sent while the prior
-- task was running ends up older than that prior task's reply, so the
-- next task claim returned an empty message list and the agent answered
-- "Tom besked".
--
-- See server/pkg/db/queries/chat.sql for the new claim query.

ALTER TABLE chat_message
    ADD COLUMN responded_at TIMESTAMPTZ;

-- Backfill: every existing user message with at least one later assistant
-- reply in the same session is marked as answered (using the earliest such
-- reply's timestamp as a best-effort approximation).
UPDATE chat_message
SET responded_at = (
    SELECT MIN(a.created_at)
      FROM chat_message a
     WHERE a.chat_session_id = chat_message.chat_session_id
       AND a.role = 'assistant'
       AND a.created_at > chat_message.created_at
)
WHERE role = 'user'
  AND EXISTS (
    SELECT 1 FROM chat_message a
     WHERE a.chat_session_id = chat_message.chat_session_id
       AND a.role = 'assistant'
       AND a.created_at > chat_message.created_at
  );

-- Daemon claim path filters by (session, role='user', responded_at IS NULL).
-- Partial index keeps it small: only un-answered user rows are read.
CREATE INDEX idx_chat_message_session_unresponded
    ON chat_message(chat_session_id, created_at)
    WHERE role = 'user' AND responded_at IS NULL;

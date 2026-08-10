-- Supports the session hard-delete path, which removes attempt rows by
-- chat_session_id alone before the session row goes.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_outbound_send_attempt_chat_session
    ON channel_outbound_send_attempt (chat_session_id)
    WHERE chat_session_id IS NOT NULL;

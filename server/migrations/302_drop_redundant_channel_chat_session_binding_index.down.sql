CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_chat_session_binding_session
    ON channel_chat_session_binding (chat_session_id);

-- Serves a conversation's wakeup list, its active-count limit and its cleanup.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_chat_wakeup_session
    ON chat_wakeup (chat_session_id, created_at DESC);

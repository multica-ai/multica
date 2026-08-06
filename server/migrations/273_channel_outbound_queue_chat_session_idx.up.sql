CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_outbound_queue_chat_session
    ON channel_outbound_queue (chat_session_id)
    WHERE chat_session_id IS NOT NULL;

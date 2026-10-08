CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS channel_chat_context_generation_session_revision_idx
    ON channel_chat_context_generation (chat_session_id, revision);

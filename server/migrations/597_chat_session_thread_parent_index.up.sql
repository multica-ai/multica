-- Finds a conversation's threads when the conversation is deleted.
CREATE INDEX CONCURRENTLY IF NOT EXISTS chat_session_thread_parent_idx
    ON chat_session (parent_chat_session_id)
    WHERE parent_chat_session_id IS NOT NULL;

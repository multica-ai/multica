-- Maps a finished chat task back to its conversation.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS conversation_agent_session_chat_uidx
    ON conversation_agent_session (chat_session_id);

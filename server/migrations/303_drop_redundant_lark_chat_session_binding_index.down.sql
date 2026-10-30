CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_lark_chat_session_binding_session
    ON lark_chat_session_binding (chat_session_id);

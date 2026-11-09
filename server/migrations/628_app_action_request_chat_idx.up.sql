-- Requests shown in a chat session.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_app_action_request_chat
    ON app_action_request (chat_session_id, created_at)
    WHERE chat_session_id IS NOT NULL;

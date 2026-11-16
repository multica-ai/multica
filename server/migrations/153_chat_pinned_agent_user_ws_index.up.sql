-- Single-statement migration: CREATE INDEX CONCURRENTLY cannot run inside a
-- transaction or a multi-command string.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_chat_pinned_agent_user_ws
    ON chat_pinned_agent (workspace_id, user_id, position);

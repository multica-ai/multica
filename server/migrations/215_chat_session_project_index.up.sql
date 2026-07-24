-- Project deletion clears chat-session references by project_id. Build this
-- partial index concurrently so the established chat_session write path stays
-- available throughout rollout. A single statement is required because
-- CREATE INDEX CONCURRENTLY cannot run inside a transaction.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_chat_session_project
    ON chat_session (project_id)
    WHERE project_id IS NOT NULL;

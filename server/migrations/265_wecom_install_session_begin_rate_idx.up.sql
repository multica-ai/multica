CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_wecom_install_session_begin_rate
    ON wecom_install_session (workspace_id, created_at, initiator_user_id);

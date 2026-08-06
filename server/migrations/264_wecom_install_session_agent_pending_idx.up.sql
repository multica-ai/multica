CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_wecom_install_session_agent_pending
    ON wecom_install_session (workspace_id, agent_id)
    WHERE status IN ('creating', 'pending');

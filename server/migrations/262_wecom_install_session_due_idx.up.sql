CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_wecom_install_session_due
    ON wecom_install_session (poll_after, created_at)
    WHERE status IN ('creating', 'pending');

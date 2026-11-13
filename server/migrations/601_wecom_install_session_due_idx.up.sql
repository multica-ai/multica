-- Drives the worker's claim scan: the oldest due session still being driven.
-- Partial on the two live states because settled sessions accumulate until the
-- retention purge and must not bloat the index the poll loop walks.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_wecom_install_session_due
    ON wecom_install_session (poll_after, created_at)
    WHERE status IN ('creating', 'pending');

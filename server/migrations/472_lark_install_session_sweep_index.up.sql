-- Supports the expiry sweep, which deletes rows that are terminal-and-past-gc
-- or pending-and-long-past-expiry. Built CONCURRENTLY per project rule; kept in
-- its own single-statement file because PostgreSQL rejects CREATE INDEX
-- CONCURRENTLY inside a transaction or a multi-command string.
CREATE INDEX CONCURRENTLY IF NOT EXISTS lark_install_session_sweep_idx
    ON lark_install_session (expires_at);

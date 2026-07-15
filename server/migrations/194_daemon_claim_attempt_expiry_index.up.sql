-- Single-statement migration: CREATE INDEX CONCURRENTLY cannot run inside a
-- transaction. Lazy expiry touches only live receipts past their deadline.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_daemon_claim_attempt_expiry
    ON daemon_claim_attempt (expires_at)
    WHERE status IN ('processing', 'ready');

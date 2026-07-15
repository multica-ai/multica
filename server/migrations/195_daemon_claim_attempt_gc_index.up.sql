-- Single-statement migration: CREATE INDEX CONCURRENTLY cannot run inside a
-- transaction. Terminal receipts use status-specific retention before lazy GC.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_daemon_claim_attempt_gc
    ON daemon_claim_attempt (updated_at)
    WHERE status IN ('acknowledged', 'expired');

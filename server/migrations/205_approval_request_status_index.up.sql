-- Pending-request lookups: the "awaiting decision" dashboard and the expiry
-- sweep both filter to status='pending' within a workspace. Partial index keeps
-- it small as the historical base grows. Keep this as the migration's only
-- statement: PostgreSQL rejects CREATE INDEX CONCURRENTLY inside a transaction
-- or multi-command string.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_approval_request_status
    ON approval_request (workspace_id, created_at)
    WHERE status = 'pending';

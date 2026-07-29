-- Approval history keyed by request (oldest first). Keep this as the
-- migration's only statement: PostgreSQL rejects CREATE INDEX CONCURRENTLY
-- inside a transaction or multi-command string.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_approval_event_request
    ON approval_event (approval_request_id, created_at);

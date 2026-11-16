-- Serves the reconciler's claim scan, which orders every state by
-- next_attempt_at and stops at the first due row. Leading with state (the
-- shape of the channel-media ledger's first claim index) would force a sort
-- once a backlog mixes states, and FOR UPDATE blocks the bounded top-N
-- optimization. CONCURRENTLY in its own file per the repo migration
-- convention.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_attachment_upload_session_due
    ON attachment_upload_session (next_attempt_at);

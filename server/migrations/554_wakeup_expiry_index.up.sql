CREATE INDEX CONCURRENTLY IF NOT EXISTS issue_wakeup_expiry_idx ON issue_wakeup(expires_at) WHERE enabled AND expires_at IS NOT NULL;

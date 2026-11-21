CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_continuation_due ON issue_continuation (next_check_at, issue_id) WHERE state <> 'resolved';

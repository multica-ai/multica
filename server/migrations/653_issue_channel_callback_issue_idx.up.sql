-- Serves event delivery and issue deletion, keyed by issue.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_channel_callback_issue
    ON issue_channel_callback (issue_id);

CREATE INDEX CONCURRENTLY idx_channel_issue_notice_pending ON channel_issue_notice(retry_at) WHERE status IN ('queued', 'sending');

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_issue_comment_notification_issue_id
    ON channel_issue_comment_notification (issue_id);

CREATE INDEX CONCURRENTLY idx_channel_notification_outbox_issue_order ON channel_notification_outbox (issue_id, created_at) WHERE status IN ('pending', 'sending');

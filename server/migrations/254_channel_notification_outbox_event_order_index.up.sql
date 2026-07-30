CREATE INDEX CONCURRENTLY idx_channel_notification_outbox_issue_event_order ON channel_notification_outbox (issue_id, created_at, event_order) WHERE status IN ('pending', 'sending');

CREATE INDEX CONCURRENTLY idx_channel_notification_outbox_pending ON channel_notification_outbox (next_attempt_at, created_at) WHERE status = 'pending';

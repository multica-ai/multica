CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_inbox_delivery_workspace
    ON channel_inbox_delivery (workspace_id, channel_type, finished_at DESC);

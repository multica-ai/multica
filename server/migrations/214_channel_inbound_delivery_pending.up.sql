CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_inbound_delivery_pending
    ON channel_inbound_delivery (available_at, created_at, id)
    WHERE status IN ('queued', 'processing');

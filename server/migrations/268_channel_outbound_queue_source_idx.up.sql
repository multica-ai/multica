CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_outbound_queue_source
    ON channel_outbound_queue (installation_id, source_kind, source_id);

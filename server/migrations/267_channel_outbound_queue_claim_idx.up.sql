CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_outbound_queue_claim
    ON channel_outbound_queue (installation_id, next_attempt_at, created_at)
    WHERE status = 'queued';

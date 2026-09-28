CREATE INDEX CONCURRENTLY IF NOT EXISTS provider_quota_pool_event_history_idx ON provider_quota_pool_event (pool_id, created_at DESC);

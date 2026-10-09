CREATE INDEX CONCURRENTLY IF NOT EXISTS provider_quota_pool_due_idx ON provider_quota_pool (state, reset_at, reset_date);

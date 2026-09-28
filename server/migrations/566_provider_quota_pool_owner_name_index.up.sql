CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS provider_quota_pool_owner_name_idx ON provider_quota_pool (owner_id, name);

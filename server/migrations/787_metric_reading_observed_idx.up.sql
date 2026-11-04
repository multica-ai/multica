-- The retention job finds readings older than its cutoff across workspaces.
CREATE INDEX CONCURRENTLY IF NOT EXISTS metric_reading_observed_idx ON metric_reading (observed_at);

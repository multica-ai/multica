-- The series of one metric at one scope, newest first: the latest reading in a
-- goal's period and the sparkline both read it.
CREATE INDEX CONCURRENTLY IF NOT EXISTS metric_reading_series_idx
    ON metric_reading (workspace_id, metric_key, scope_type, scope_id, observed_at DESC);

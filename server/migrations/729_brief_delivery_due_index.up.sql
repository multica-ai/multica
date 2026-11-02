-- The retry job's scan: rows in a sendable state, oldest attempt first.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_brief_delivery_state_next_attempt ON brief_delivery (state, next_attempt_at);

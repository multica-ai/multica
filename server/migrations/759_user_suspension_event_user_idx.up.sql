CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_user_suspension_event_user ON user_suspension_event (user_id, created_at DESC);

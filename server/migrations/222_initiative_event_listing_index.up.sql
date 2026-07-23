CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_initiative_event_listing ON initiative_event (initiative_id, created_at DESC, id DESC);

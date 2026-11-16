-- Serves the reconciler's claim scan (state + due time). The migration runner
-- executes files outside an explicit transaction, so CONCURRENTLY is kept in
-- its own single-statement migration to avoid Postgres' implicit transaction
-- block for multi-statement query strings (same convention as migration 119).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_media_pending_object_claim
    ON channel_media_pending_object (state, next_attempt_at);

-- pg_bigm deployments restore the preferred index in migration 454 instead.
-- The runner executes this only where pg_bigm is unavailable.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_comment_content_trgm
    ON comment USING gin (LOWER(content) gin_trgm_ops);

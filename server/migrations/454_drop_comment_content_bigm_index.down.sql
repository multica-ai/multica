-- Restore migration 036's preferred comment search index where pg_bigm is
-- available. The migration runner skips this statement in other environments.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_comment_content_bigm
    ON comment USING gin (LOWER(content) gin_bigm_ops);

-- Restore the portable fallback when rolling back the mutually exclusive
-- comment search index strategy.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_comment_content_trgm
    ON comment USING gin (LOWER(content) gin_trgm_ops);

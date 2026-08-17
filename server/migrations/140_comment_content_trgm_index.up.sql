-- Fallback GIN index for SearchIssues comment content LIKE matches when
-- pg_bigm is not available. Single-statement file required for CREATE INDEX
-- CONCURRENTLY.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_comment_content_trgm
    ON comment USING gin (LOWER(content) gin_trgm_ops);

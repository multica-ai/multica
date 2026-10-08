-- Restore migration 036's CJK-friendly description search index where
-- pg_bigm is available. The migration runner skips this statement elsewhere.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_description_bigm
    ON issue USING gin (LOWER(COALESCE(description, '')) gin_bigm_ops);

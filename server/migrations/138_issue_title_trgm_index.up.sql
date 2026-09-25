-- Fallback GIN index for SearchIssues title LIKE matches when pg_bigm is not
-- available. This must stay as a single-statement migration because CREATE
-- INDEX CONCURRENTLY cannot run inside a transaction or multi-command string.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_title_trgm
    ON issue USING gin (LOWER(title) gin_trgm_ops);

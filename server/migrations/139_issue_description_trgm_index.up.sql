-- Fallback GIN index for SearchIssues description LIKE matches when pg_bigm is
-- not available. Single-statement file required for CREATE INDEX CONCURRENTLY.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_description_trgm
    ON issue USING gin (LOWER(COALESCE(description, '')) gin_trgm_ops);

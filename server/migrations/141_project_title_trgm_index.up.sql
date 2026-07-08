-- Fallback GIN index for SearchProjects title LIKE matches when pg_bigm is not
-- available. Single-statement file required for CREATE INDEX CONCURRENTLY.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_project_title_trgm
    ON project USING gin (LOWER(title) gin_trgm_ops);

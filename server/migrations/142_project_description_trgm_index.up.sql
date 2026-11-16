-- Fallback GIN index for SearchProjects description LIKE matches when pg_bigm
-- is not available. Single-statement file required for CREATE INDEX
-- CONCURRENTLY.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_project_description_trgm
    ON project USING gin (LOWER(COALESCE(description, '')) gin_trgm_ops);

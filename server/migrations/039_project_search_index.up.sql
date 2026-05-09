-- Add GIN bigram indexes on project title and description for search.
DO $$
BEGIN
  CREATE INDEX idx_project_title_bigm ON project USING gin (LOWER(title) gin_bigm_ops);
  CREATE INDEX idx_project_description_bigm ON project USING gin (LOWER(COALESCE(description, '')) gin_bigm_ops);
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'skipping bigram indexes on project (pg_bigm not installed)';
END
$$;

-- Drop the pg_trgm fallback GIN search indexes. Leaves the pg_trgm
-- extension in place because other future queries may depend on it and
-- dropping it has no benefit (extension idle overhead is essentially
-- zero). The pg_bigm indexes from migrations 036 / 039 are untouched.

DROP INDEX IF EXISTS idx_issue_title_trgm;
DROP INDEX IF EXISTS idx_issue_description_trgm;
DROP INDEX IF EXISTS idx_comment_content_trgm;
DROP INDEX IF EXISTS idx_project_title_trgm;
DROP INDEX IF EXISTS idx_project_description_trgm;

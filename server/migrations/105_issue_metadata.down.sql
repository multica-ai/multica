DROP INDEX IF EXISTS idx_issue_metadata_gin;
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_metadata_size_limit;
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_metadata_is_object;
ALTER TABLE issue DROP COLUMN IF EXISTS metadata;

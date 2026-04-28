DROP INDEX IF EXISTS idx_artifact_origin_issue;
ALTER TABLE artifact DROP COLUMN IF EXISTS origin_issue_id;

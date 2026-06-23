DROP INDEX IF EXISTS idx_issue_stage_id;
ALTER TABLE multica_issue DROP COLUMN IF EXISTS stage_id;

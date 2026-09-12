DROP TRIGGER IF EXISTS issue_stage_generation ON issue;
DROP FUNCTION IF EXISTS advance_issue_stage_generation();
ALTER TABLE issue DROP COLUMN IF EXISTS stage_generation;

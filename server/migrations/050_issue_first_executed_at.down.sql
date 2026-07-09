DROP INDEX IF EXISTS idx_issue_first_executed_at;
ALTER TABLE issue DROP COLUMN IF EXISTS first_executed_at;

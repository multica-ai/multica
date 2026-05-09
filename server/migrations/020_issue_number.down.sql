DROP INDEX IF EXISTS idx_issue_workspace_number;
ALTER TABLE issue DROP CONSTRAINT IF EXISTS uq_issue_workspace_number;
ALTER TABLE issue DROP COLUMN IF EXISTS number;
ALTER TABLE workspace DROP COLUMN IF EXISTS issue_prefix;
ALTER TABLE workspace DROP COLUMN IF EXISTS issue_counter;

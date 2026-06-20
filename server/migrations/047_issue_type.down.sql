DROP INDEX IF EXISTS idx_issue_workspace_type;
ALTER TABLE issue DROP COLUMN IF EXISTS issue_type_id;

DROP INDEX IF EXISTS idx_issue_type_workspace_active;
DROP TABLE IF EXISTS issue_type;

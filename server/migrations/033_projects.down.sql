DROP INDEX IF EXISTS idx_issue_project;
ALTER TABLE issue DROP COLUMN IF EXISTS project_id;

DROP INDEX IF EXISTS idx_project_workspace;
DROP TABLE IF EXISTS project;
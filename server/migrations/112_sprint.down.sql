-- Rollback sprint feature
DROP INDEX IF EXISTS sprint_project_workspace;
DROP INDEX IF EXISTS sprint_issue_sprint_id;
ALTER TABLE issue DROP COLUMN IF EXISTS sprint_id;
DROP INDEX IF EXISTS sprint_one_active_per_project;
DROP TABLE IF EXISTS sprint;
ALTER TABLE issue DROP COLUMN IF EXISTS estimate;

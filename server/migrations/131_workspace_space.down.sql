DROP INDEX IF EXISTS idx_autopilot_workspace_space;
ALTER TABLE autopilot DROP CONSTRAINT IF EXISTS fk_autopilot_workspace_space;
ALTER TABLE autopilot DROP COLUMN IF EXISTS space_id;

DROP INDEX IF EXISTS idx_issue_project_space;
DROP INDEX IF EXISTS idx_issue_workspace_space_created_at;
DROP INDEX IF EXISTS idx_issue_workspace_space_status_position;
ALTER TABLE issue DROP CONSTRAINT IF EXISTS fk_issue_workspace_space;
ALTER TABLE issue DROP COLUMN IF EXISTS space_id;

DROP TABLE IF EXISTS project_space;
ALTER TABLE project DROP CONSTRAINT IF EXISTS uq_project_workspace_id;

DROP TABLE IF EXISTS workspace_space_member;

DROP INDEX IF EXISTS idx_workspace_space_active;
DROP INDEX IF EXISTS uq_workspace_space_default;
DROP INDEX IF EXISTS uq_workspace_space_workspace_key_lower;
DROP TABLE IF EXISTS workspace_space;

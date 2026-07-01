DROP INDEX IF EXISTS idx_autopilot_workspace_team;
ALTER TABLE autopilot DROP CONSTRAINT IF EXISTS fk_autopilot_workspace_team;
ALTER TABLE autopilot DROP COLUMN IF EXISTS team_id;

DROP INDEX IF EXISTS idx_issue_project_team;
DROP INDEX IF EXISTS idx_issue_workspace_team_created_at;
DROP INDEX IF EXISTS idx_issue_workspace_team_status_position;
ALTER TABLE issue DROP CONSTRAINT IF EXISTS fk_issue_workspace_team;
ALTER TABLE issue DROP COLUMN IF EXISTS team_id;

DROP TABLE IF EXISTS project_team;
ALTER TABLE project DROP CONSTRAINT IF EXISTS uq_project_workspace_id;

DROP TABLE IF EXISTS workspace_team_member;

DROP INDEX IF EXISTS idx_workspace_team_active;
DROP INDEX IF EXISTS uq_workspace_team_default;
DROP INDEX IF EXISTS uq_workspace_team_workspace_key_lower;
DROP TABLE IF EXISTS workspace_team;

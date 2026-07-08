DROP INDEX IF EXISTS idx_autopilot_project;

ALTER TABLE autopilot
    DROP COLUMN IF EXISTS project_id;

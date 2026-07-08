ALTER TABLE autopilot
    ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES project(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_autopilot_project ON autopilot(project_id);

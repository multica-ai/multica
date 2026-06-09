ALTER TABLE autopilot ADD COLUMN project_id UUID REFERENCES project(id) ON DELETE SET NULL;
ALTER TABLE autopilot ADD COLUMN priority TEXT NOT NULL DEFAULT 'medium'
    CHECK (priority IN ('urgent', 'high', 'medium', 'low', 'none'));

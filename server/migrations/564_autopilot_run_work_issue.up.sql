-- The issue a run's agent declared it is working on (run_only runs pick their
-- own issue). Display pointer only: no foreign key, and readers tolerate an
-- issue that has since been deleted.
ALTER TABLE autopilot_run ADD COLUMN IF NOT EXISTS work_issue_id UUID;

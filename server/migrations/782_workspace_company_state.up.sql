-- The company's operating state (spec 4.5): running, paused or quiet, and the
-- daily quiet-hours window in the workspace timezone. company_state_updated_at
-- is the last change to the state or the window, reported to the boss.
--
-- The CHECK is added NOT VALID and validated after, as in 769, so the table is
-- checked without holding a long lock while the constraint is validated.
ALTER TABLE workspace ADD COLUMN IF NOT EXISTS company_state TEXT NOT NULL DEFAULT 'running';
ALTER TABLE workspace ADD COLUMN IF NOT EXISTS company_state_updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE workspace ADD COLUMN IF NOT EXISTS quiet_enabled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE workspace ADD COLUMN IF NOT EXISTS quiet_start TIME NOT NULL DEFAULT '22:00';
ALTER TABLE workspace ADD COLUMN IF NOT EXISTS quiet_end TIME NOT NULL DEFAULT '07:00';
ALTER TABLE workspace ADD COLUMN IF NOT EXISTS quiet_timezone TEXT NOT NULL DEFAULT 'UTC';
ALTER TABLE workspace DROP CONSTRAINT IF EXISTS workspace_company_state_check;
ALTER TABLE workspace ADD CONSTRAINT workspace_company_state_check
    CHECK (company_state IN ('running', 'paused', 'quiet')) NOT VALID;
ALTER TABLE workspace VALIDATE CONSTRAINT workspace_company_state_check;

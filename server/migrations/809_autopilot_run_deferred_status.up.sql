-- A routine run that quiet hours defer is stored as status deferred, with
-- reason_code quiet_hours, until the window closes (spec 4.5). It is neither
-- skipped nor pending: nothing has been refused, and nothing has started.
--
-- Added NOT VALID and validated after, as in 769, so the table is checked
-- without holding a long lock while the constraint is validated.
ALTER TABLE autopilot_run DROP CONSTRAINT IF EXISTS autopilot_run_status_check;
ALTER TABLE autopilot_run ADD CONSTRAINT autopilot_run_status_check
    CHECK (status IN ('issue_created', 'running', 'completed', 'failed', 'skipped', 'deferred')) NOT VALID;
ALTER TABLE autopilot_run VALIDATE CONSTRAINT autopilot_run_status_check;

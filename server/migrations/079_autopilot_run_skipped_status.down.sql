-- Migrate any 'skipped' rows to 'failed' before tightening the constraint
-- (mirrors what 043 did for the original removal).
UPDATE autopilot_run
SET status = 'failed',
    completed_at = COALESCE(completed_at, now()),
    failure_reason = COALESCE(failure_reason, 'migrated from skipped status')
WHERE status = 'skipped';

ALTER TABLE autopilot_run DROP CONSTRAINT IF EXISTS autopilot_run_status_check;
ALTER TABLE autopilot_run ADD CONSTRAINT autopilot_run_status_check
    CHECK (status IN ('issue_created', 'running', 'completed', 'failed'));

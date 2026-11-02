-- Catch-up runs cannot be kept under the old constraint. They are re-labelled
-- 'schedule' (the trigger that the catch-up fired) before the value is removed.
UPDATE autopilot_run SET source = 'schedule' WHERE source = 'catchup';

ALTER TABLE autopilot_run DROP CONSTRAINT IF EXISTS autopilot_run_source_check;
ALTER TABLE autopilot_run ADD CONSTRAINT autopilot_run_source_check
    CHECK (source IN ('schedule', 'manual', 'webhook', 'api'));

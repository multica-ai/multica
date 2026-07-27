-- Revert the source CHECK to the original four values.
ALTER TABLE autopilot_run DROP CONSTRAINT IF EXISTS autopilot_run_source_check;
ALTER TABLE autopilot_run ADD CONSTRAINT autopilot_run_source_check
    CHECK (source IN ('schedule', 'manual', 'webhook', 'api'));

DROP TABLE IF EXISTS autopilot_successor;

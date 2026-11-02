-- The catch-up job (spec 2.9, plan 9.13) fires the daily brief with source
-- 'catchup', so the audit trail shows why a run happened. The constraint is
-- replaced with the new value. It is added NOT VALID and then validated, so the
-- table is scanned under a lock that lets reads and writes go on.
ALTER TABLE autopilot_run DROP CONSTRAINT IF EXISTS autopilot_run_source_check;
ALTER TABLE autopilot_run ADD CONSTRAINT autopilot_run_source_check
    CHECK (source IN ('schedule', 'manual', 'webhook', 'api', 'catchup')) NOT VALID;
ALTER TABLE autopilot_run VALIDATE CONSTRAINT autopilot_run_source_check;

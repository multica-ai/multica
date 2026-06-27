DROP INDEX IF EXISTS uq_autopilot_run_trigger_planned;

ALTER TABLE autopilot_run
    DROP COLUMN IF EXISTS planned_at;

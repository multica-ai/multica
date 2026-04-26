DROP INDEX IF EXISTS idx_autopilot_trigger_due;

ALTER TABLE autopilot_trigger
    DROP CONSTRAINT IF EXISTS autopilot_trigger_schedule_cron_required;

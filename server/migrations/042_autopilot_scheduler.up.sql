ALTER TABLE autopilot_trigger
    ADD CONSTRAINT autopilot_trigger_schedule_cron_required
    CHECK (type <> 'schedule' OR cron IS NOT NULL);

CREATE INDEX idx_autopilot_trigger_due
    ON autopilot_trigger(next_run_at)
    WHERE type = 'schedule' AND status = 'active' AND next_run_at IS NOT NULL;

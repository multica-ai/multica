ALTER TABLE cerebro_sprint_recurring_task
    DROP COLUMN IF EXISTS sprint_day_offset;

ALTER TABLE cerebro_sprint_settings
    DROP COLUMN IF EXISTS move_incomplete_target_status;

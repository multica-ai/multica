ALTER TABLE task_usage_hourly_dirty
    DROP COLUMN IF EXISTS squad_id;

ALTER TABLE task_usage_hourly
    DROP COLUMN IF EXISTS squad_id;

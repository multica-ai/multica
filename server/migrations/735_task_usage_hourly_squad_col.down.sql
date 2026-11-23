ALTER TABLE task_usage_hourly_dirty
    DROP COLUMN IF EXISTS squad_id;

ALTER TABLE task_usage_hourly
    DROP COLUMN IF EXISTS squad_id;

ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS usage_squad_id;

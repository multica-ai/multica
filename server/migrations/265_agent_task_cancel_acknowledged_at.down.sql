ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS cancel_acknowledged_at;

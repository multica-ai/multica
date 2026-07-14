ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS last_heartbeat_at;

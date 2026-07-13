DROP INDEX IF EXISTS idx_agent_task_queue_running_heartbeat;

ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS last_heartbeat_at;

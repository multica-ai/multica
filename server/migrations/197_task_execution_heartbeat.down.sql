-- Reverts the task-scoped backend liveness timestamp.
ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS last_heartbeat_at;

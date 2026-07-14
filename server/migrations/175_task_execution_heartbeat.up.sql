-- Stores the most recent observed backend message for a task. Runtime
-- heartbeats only prove the daemon process is up; a worker goroutine can still
-- be wedged while its runtime continues serving other tasks.
ALTER TABLE agent_task_queue
    ADD COLUMN IF NOT EXISTS last_heartbeat_at TIMESTAMPTZ;

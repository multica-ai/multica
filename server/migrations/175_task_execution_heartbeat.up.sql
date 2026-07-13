-- Tracks liveness of the daemon worker executing a specific task. Runtime
-- heartbeats only prove the daemon process is up; a worker goroutine can still
-- be wedged while its runtime continues serving other tasks.
ALTER TABLE agent_task_queue
    ADD COLUMN IF NOT EXISTS last_heartbeat_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_agent_task_queue_running_heartbeat
    ON agent_task_queue (last_heartbeat_at)
    WHERE status = 'running';

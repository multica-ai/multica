-- Reverts the concurrently created task-heartbeat index.
DROP INDEX CONCURRENTLY IF EXISTS idx_agent_task_queue_running_heartbeat;

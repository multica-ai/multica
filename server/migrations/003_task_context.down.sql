ALTER TABLE daemon_connection DROP CONSTRAINT IF EXISTS uq_daemon_agent;
DROP INDEX IF EXISTS idx_agent_task_queue_pending;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS context;

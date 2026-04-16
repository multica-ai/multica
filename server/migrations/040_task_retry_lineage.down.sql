DROP INDEX IF EXISTS idx_agent_task_queue_retried_from;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS retried_from_id;

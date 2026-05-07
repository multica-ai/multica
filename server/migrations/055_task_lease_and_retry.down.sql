DROP INDEX IF EXISTS idx_agent_task_queue_parent;

ALTER TABLE agent_task_queue
  DROP COLUMN IF EXISTS attempt,
  DROP COLUMN IF EXISTS max_attempts,
  DROP COLUMN IF EXISTS parent_task_id,
  DROP COLUMN IF EXISTS failure_reason,
  DROP COLUMN IF EXISTS last_heartbeat_at;

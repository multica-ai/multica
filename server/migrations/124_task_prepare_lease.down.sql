DROP INDEX IF EXISTS idx_agent_task_queue_dispatched_prepare;

ALTER TABLE agent_task_queue
  DROP COLUMN IF EXISTS prepare_lease_expires_at;

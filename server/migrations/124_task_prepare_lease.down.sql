ALTER TABLE agent_task_queue
  DROP COLUMN IF EXISTS prepare_lease_expires_at;

ALTER TABLE agent_task_queue
  DROP COLUMN IF EXISTS run_lease_expires_at;

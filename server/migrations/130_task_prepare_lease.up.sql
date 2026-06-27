ALTER TABLE agent_task_queue
  ADD COLUMN IF NOT EXISTS prepare_lease_expires_at TIMESTAMPTZ;

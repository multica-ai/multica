ALTER TABLE agent_task_queue
  ADD COLUMN prepare_lease_expires_at TIMESTAMPTZ;

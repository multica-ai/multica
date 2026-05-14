ALTER TABLE agent_task_queue
  ADD COLUMN IF NOT EXISTS last_heartbeat_at TIMESTAMPTZ;

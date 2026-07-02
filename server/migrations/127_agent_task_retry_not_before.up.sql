ALTER TABLE agent_task_queue
  ADD COLUMN not_before TIMESTAMPTZ,
  ALTER COLUMN max_attempts SET DEFAULT 4;

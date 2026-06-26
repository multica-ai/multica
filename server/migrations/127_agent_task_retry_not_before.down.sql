ALTER TABLE agent_task_queue
  ALTER COLUMN max_attempts SET DEFAULT 2,
  DROP COLUMN IF EXISTS not_before;

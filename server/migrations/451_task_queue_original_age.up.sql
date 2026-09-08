ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS original_queued_at timestamptz;

ALTER TABLE agent_task_queue
ADD COLUMN IF NOT EXISTS quick_actions_disabled BOOLEAN NOT NULL DEFAULT false;

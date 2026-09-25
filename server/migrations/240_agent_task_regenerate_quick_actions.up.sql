ALTER TABLE agent_task_queue
ADD COLUMN IF NOT EXISTS regenerate_quick_actions_for UUID;

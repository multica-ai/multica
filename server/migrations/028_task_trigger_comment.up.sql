-- CEREBRO-PATCH(migration-idempotent-028-task-trigger-comment): cerebro modification of upstream file
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS trigger_comment_id UUID REFERENCES comment(id) ON DELETE SET NULL;

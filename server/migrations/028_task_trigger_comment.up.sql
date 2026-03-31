ALTER TABLE agent_task_queue ADD COLUMN trigger_comment_id UUID REFERENCES comment(id) ON DELETE SET NULL;

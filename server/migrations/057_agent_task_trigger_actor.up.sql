ALTER TABLE agent_task_queue
ADD COLUMN trigger_source TEXT,
ADD COLUMN trigger_actor_type TEXT CHECK (trigger_actor_type IN ('member', 'agent', 'system')),
ADD COLUMN trigger_actor_id UUID;

ALTER TABLE workflow_node_run ADD COLUMN IF NOT EXISTS worker_agent_task_id UUID REFERENCES agent_task_queue(id);
ALTER TABLE workflow_node_run ADD COLUMN IF NOT EXISTS critic_agent_task_id UUID REFERENCES agent_task_queue(id);

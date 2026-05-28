ALTER TABLE workflow_node_run DROP COLUMN IF EXISTS worker_agent_task_id;
ALTER TABLE workflow_node_run DROP COLUMN IF EXISTS critic_agent_task_id;

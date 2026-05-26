ALTER TABLE workflow_node_run DROP CONSTRAINT IF EXISTS workflow_node_run_worker_agent_task_id_fkey;
ALTER TABLE workflow_node_run DROP CONSTRAINT IF EXISTS workflow_node_run_critic_agent_task_id_fkey;
ALTER TABLE workflow_node_run ADD CONSTRAINT workflow_node_run_worker_agent_task_id_fkey FOREIGN KEY (worker_agent_task_id) REFERENCES agent_task_queue(id) ON DELETE SET NULL;
ALTER TABLE workflow_node_run ADD CONSTRAINT workflow_node_run_critic_agent_task_id_fkey FOREIGN KEY (critic_agent_task_id) REFERENCES agent_task_queue(id) ON DELETE SET NULL;

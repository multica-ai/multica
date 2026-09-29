DROP TRIGGER IF EXISTS trg_agent_task_apply_workspace_max_attempts ON agent_task_queue;
DROP FUNCTION IF EXISTS agent_task_apply_workspace_max_attempts();

ALTER TABLE agent_task_queue
  ALTER COLUMN max_attempts SET DEFAULT 2;

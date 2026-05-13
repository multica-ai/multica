DROP INDEX IF EXISTS idx_agent_task_queue_target_project_resource;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS target_project_resource_id;

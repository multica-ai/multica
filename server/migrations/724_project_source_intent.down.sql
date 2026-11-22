DROP TRIGGER project_resource_source_intent ON project_resource;
DROP FUNCTION sync_project_source_intent();
DROP TRIGGER project_source_intent_check ON project;
DROP FUNCTION check_project_source_intent();
ALTER TABLE agent_task_queue DROP COLUMN execution_source;
ALTER TABLE project DROP COLUMN source_intent;

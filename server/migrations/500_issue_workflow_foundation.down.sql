ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS automation_execution_id;
ALTER TABLE issue DROP COLUMN IF EXISTS last_transition_id;
ALTER TABLE issue DROP COLUMN IF EXISTS workflow_status_id;
ALTER TABLE issue DROP COLUMN IF EXISTS workflow_id;
ALTER TABLE project DROP COLUMN IF EXISTS default_issue_workflow_id;
ALTER TABLE workspace DROP COLUMN IF EXISTS default_issue_workflow_id;

DROP TABLE IF EXISTS automation_execution;
DROP TABLE IF EXISTS issue_transition;
DROP TABLE IF EXISTS issue_workflow_status;
DROP TABLE IF EXISTS issue_workflow;

-- Reverse 068: drop agent_workflow_run + agent_workflow. Idempotent.
DROP INDEX IF EXISTS idx_agent_workflow_run_issue;
DROP INDEX IF EXISTS idx_agent_workflow_run_task;
DROP INDEX IF EXISTS idx_agent_workflow_run_status;
DROP INDEX IF EXISTS idx_agent_workflow_run_workspace;
DROP INDEX IF EXISTS idx_agent_workflow_run_workflow;
DROP TABLE IF EXISTS agent_workflow_run;

DROP INDEX IF EXISTS idx_agent_workflow_agent;
DROP INDEX IF EXISTS idx_agent_workflow_workspace;
DROP TABLE IF EXISTS agent_workflow;

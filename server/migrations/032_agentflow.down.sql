ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS agentflow_run_id;
ALTER TABLE agent_task_queue ALTER COLUMN issue_id SET NOT NULL;

DROP TABLE IF EXISTS agentflow_run;
DROP TABLE IF EXISTS agentflow_trigger;
DROP TABLE IF EXISTS agentflow;

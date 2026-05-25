ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS workflow_node_run_id;

DROP TABLE IF EXISTS workflow_node_run;
DROP TABLE IF EXISTS workflow_run;
DROP TABLE IF EXISTS workflow_edge;
DROP TABLE IF EXISTS workflow_node;
DROP TABLE IF EXISTS workflow;

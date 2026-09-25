ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS trigger_evidence_ref_id;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS trigger_evidence_kind;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS rule_version_id;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS rerun_of_task_id;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS retry_of_task_id;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS delegated_from_task_id;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS originator_source;

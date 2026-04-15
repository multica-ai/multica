DROP INDEX IF EXISTS idx_issue_origin;
ALTER TABLE issue DROP COLUMN IF EXISTS origin_id;
ALTER TABLE issue DROP COLUMN IF EXISTS origin_type;

ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS autopilot_run_id;

DROP TABLE IF EXISTS autopilot_run;
DROP TABLE IF EXISTS autopilot_trigger;
DROP TABLE IF EXISTS autopilot;

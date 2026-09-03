DROP TRIGGER IF EXISTS issue_workflow_order_guard_write ON issue;
DROP FUNCTION IF EXISTS enforce_issue_workflow_stage_order();
DROP TABLE IF EXISTS workflow_transition;
DROP TABLE IF EXISTS workflow_run;
DROP TABLE IF EXISTS workflow_definition;

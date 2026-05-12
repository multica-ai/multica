-- Rollback for 9020_cerebro_workflows.up.sql.
-- Drop child tables before parents. CASCADE protects against any forgotten FK.

DROP TABLE IF EXISTS cerebro_issue_due_time;
DROP TABLE IF EXISTS cerebro_workflow_idempotency_key;
DROP TABLE IF EXISTS cerebro_workflow_run;
DROP TABLE IF EXISTS cerebro_workflow;

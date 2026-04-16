-- Rollback orchestrated workflows

ALTER TABLE workflow_step_run DROP COLUMN IF EXISTS redirect_to_step;

ALTER TABLE workflow_step_run DROP CONSTRAINT IF EXISTS workflow_step_run_decision_check;
ALTER TABLE workflow_step_run ADD CONSTRAINT workflow_step_run_decision_check
    CHECK (decision IN ('approved', 'rejected', 'stopped'));

ALTER TABLE workflow_step_run DROP CONSTRAINT IF EXISTS workflow_step_run_step_type_check;
ALTER TABLE workflow_step_run ADD CONSTRAINT workflow_step_run_step_type_check
    CHECK (step_type IN ('agent', 'approval'));

ALTER TABLE workflow_run DROP CONSTRAINT IF EXISTS workflow_run_status_check;
ALTER TABLE workflow_run ADD CONSTRAINT workflow_run_status_check
    CHECK (status IN ('pending', 'running', 'paused', 'completed', 'failed', 'cancelled'));

DROP INDEX IF EXISTS idx_workflow_run_planner_task;

ALTER TABLE workflow_run DROP COLUMN IF EXISTS dynamic_steps;
ALTER TABLE workflow_run DROP COLUMN IF EXISTS planner_task_id;

ALTER TABLE workflow DROP COLUMN IF EXISTS planner_agent_id;
ALTER TABLE workflow DROP COLUMN IF EXISTS mode;

DROP INDEX IF EXISTS idx_workflow_run_status;
CREATE INDEX idx_workflow_run_status ON workflow_run(workspace_id, status) WHERE status IN ('pending', 'running', 'paused');

-- Phase 1: Orchestrated workflows with review steps
-- (Idempotent — safe to re-run)

-- Add mode and planner agent to workflow table
ALTER TABLE workflow
    ADD COLUMN IF NOT EXISTS mode TEXT NOT NULL DEFAULT 'sequential',
    ADD COLUMN IF NOT EXISTS planner_agent_id UUID REFERENCES agent(id) ON DELETE SET NULL;

-- Add dynamic steps and planner task to workflow run
ALTER TABLE workflow_run
    ADD COLUMN IF NOT EXISTS dynamic_steps JSONB,
    ADD COLUMN IF NOT EXISTS planner_task_id UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL;

-- Allow 'planning' status for workflow runs
ALTER TABLE workflow_run DROP CONSTRAINT IF EXISTS workflow_run_status_check;
ALTER TABLE workflow_run ADD CONSTRAINT workflow_run_status_check
    CHECK (status IN ('pending', 'running', 'paused', 'completed', 'failed', 'cancelled', 'planning'));

-- Allow 'review' and 'planner' step types in step runs
ALTER TABLE workflow_step_run DROP CONSTRAINT IF EXISTS workflow_step_run_step_type_check;
ALTER TABLE workflow_step_run ADD CONSTRAINT workflow_step_run_step_type_check
    CHECK (step_type IN ('agent', 'approval', 'review', 'planner'));

-- Allow 'redirect' decision value
ALTER TABLE workflow_step_run DROP CONSTRAINT IF EXISTS workflow_step_run_decision_check;
ALTER TABLE workflow_step_run ADD CONSTRAINT workflow_step_run_decision_check
    CHECK (decision IN ('approved', 'rejected', 'stopped', 'redirect'));

-- Add redirect target step index
ALTER TABLE workflow_step_run
    ADD COLUMN IF NOT EXISTS redirect_to_step INT;

-- Index for looking up runs by planner task
CREATE INDEX IF NOT EXISTS idx_workflow_run_planner_task ON workflow_run(planner_task_id) WHERE planner_task_id IS NOT NULL;

-- Update the status index to include planning
DROP INDEX IF EXISTS idx_workflow_run_status;
CREATE INDEX idx_workflow_run_status ON workflow_run(workspace_id, status) WHERE status IN ('pending', 'running', 'paused', 'planning');

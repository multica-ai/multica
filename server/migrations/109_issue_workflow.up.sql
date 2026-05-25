-- Link issues to workflows and workflow runs.
-- When an issue is assigned to a workflow, the backend creates a WorkflowRun
-- and stamps the issue with both the workflow_id and the run_id.
ALTER TABLE issue ADD COLUMN workflow_id UUID REFERENCES workflow(id) ON DELETE SET NULL;
ALTER TABLE issue ADD COLUMN workflow_run_id UUID REFERENCES workflow_run(id) ON DELETE SET NULL;

CREATE INDEX idx_issue_workflow_run_id ON issue(workflow_run_id);

-- Extend assignee_type CHECK to include 'workflow'.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_assignee_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_assignee_type_check
    CHECK (assignee_type IN ('member', 'agent', 'squad', 'workflow'));

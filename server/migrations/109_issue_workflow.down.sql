DROP INDEX IF EXISTS idx_issue_workflow_run_id;

ALTER TABLE issue DROP COLUMN IF EXISTS workflow_run_id;
ALTER TABLE issue DROP COLUMN IF EXISTS workflow_id;

-- Restore assignee_type CHECK to pre-109 state.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_assignee_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_assignee_type_check
    CHECK (assignee_type IN ('member', 'agent', 'squad'));

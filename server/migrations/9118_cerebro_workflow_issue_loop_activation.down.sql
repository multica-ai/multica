DROP INDEX IF EXISTS idx_cerebro_workflow_activation_unique;
DROP INDEX IF EXISTS idx_cerebro_workflow_generated_for_issue;

ALTER TABLE cerebro_workflow
    DROP COLUMN IF EXISTS generated_for_issue_id;

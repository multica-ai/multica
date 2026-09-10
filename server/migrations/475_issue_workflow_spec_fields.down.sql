ALTER TABLE issue_workflow_status
    DROP CONSTRAINT IF EXISTS issue_workflow_status_spec_key_format,
    DROP COLUMN IF EXISTS spec_key;

ALTER TABLE issue_workflow
    DROP COLUMN IF EXISTS initial_status_id;

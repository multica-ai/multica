CREATE UNIQUE INDEX CONCURRENTLY idx_issue_workflow_status_spec_key ON issue_workflow_status(workflow_id, spec_key);

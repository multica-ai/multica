CREATE UNIQUE INDEX CONCURRENTLY idx_issue_workflow_status_legacy_key ON issue_workflow_status(workflow_id, legacy_status_key) WHERE legacy_status_key IS NOT NULL;

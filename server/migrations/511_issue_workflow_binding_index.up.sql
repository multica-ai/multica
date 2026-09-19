CREATE INDEX CONCURRENTLY idx_issue_workflow_binding ON issue(workspace_id, workflow_id, workflow_status_id);

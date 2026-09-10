CREATE UNIQUE INDEX CONCURRENTLY idx_issue_workflow_scope ON issue_workflow(workspace_id, scope_type, scope_id);

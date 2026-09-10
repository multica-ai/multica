CREATE UNIQUE INDEX CONCURRENTLY idx_issue_lifecycle_scope ON issue_lifecycle(workspace_id, scope_type, scope_id);

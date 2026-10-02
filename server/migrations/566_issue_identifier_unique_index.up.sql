CREATE UNIQUE INDEX CONCURRENTLY uq_issue_workspace_prefix_number ON issue(workspace_id, identifier_prefix, number);

-- Partial index for the "shared with workspace" branch of the list query.
CREATE INDEX CONCURRENTLY idx_issue_view_shared
    ON issue_view (workspace_id, scope_type, scope_id)
    WHERE visibility = 'workspace';

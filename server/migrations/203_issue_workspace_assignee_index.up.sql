-- Server-side Table grouping filters by workspace before grouping assignees.
-- Keep the concurrent build isolated so writes to the hot issue table remain
-- available while existing workspaces are indexed.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_workspace_assignee
    ON issue (workspace_id, assignee_type, assignee_id);

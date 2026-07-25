-- Root/child Table branches always resolve hierarchy inside one workspace.
-- The existing parent-only index cannot prune another workspace first.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_workspace_parent
    ON issue (workspace_id, parent_issue_id);

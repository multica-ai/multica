-- Recursive thread expansion repeatedly looks up same-tenant children by
-- parent_id. Keep this as the migration's only statement: production index
-- builds must be concurrent so comment writes remain available.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_comment_workspace_issue_parent
    ON comment (workspace_id, issue_id, parent_id)
    WHERE parent_id IS NOT NULL;

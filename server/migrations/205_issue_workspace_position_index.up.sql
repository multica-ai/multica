-- Serve the Table API's default keyset order without scanning and sorting the
-- entire workspace on every page.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_workspace_position
    ON issue (workspace_id, position, created_at DESC, id DESC);

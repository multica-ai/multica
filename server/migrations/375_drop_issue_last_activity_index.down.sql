-- Restore the serving index when rolling back the retirement migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_workspace_last_activity
    ON issue (workspace_id, last_activity_at DESC NULLS LAST, id DESC);

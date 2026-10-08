-- The recent-activity issue window is not active, while maintaining this index
-- makes every last_activity_at update ineligible for a HOT update.
DROP INDEX CONCURRENTLY IF EXISTS idx_issue_workspace_last_activity;

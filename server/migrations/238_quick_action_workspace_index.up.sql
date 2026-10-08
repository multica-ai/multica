-- Listing index for the settings page and the issue sidebar (MUL-5465). Both
-- read "the active actions of one workspace", ordered by use_count DESC.
-- Single-statement migration: CREATE INDEX CONCURRENTLY cannot run inside a
-- transaction or share a migration file.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_quick_action_workspace_status_usage
    ON quick_action(workspace_id, status, use_count DESC);

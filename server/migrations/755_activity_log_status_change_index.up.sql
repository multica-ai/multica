-- The contribution dashboard scans one workspace's status changes inside a
-- date window; without this every request walks the whole activity log.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_activity_log_workspace_status_changed
    ON activity_log (workspace_id, created_at)
    WHERE action = 'status_changed';

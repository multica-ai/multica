-- Helps the two lazy endpoints (ListRuntimeUsageByAgent / GetRuntimeUsageByHour)
-- that still scan the raw `task_usage` table by created_at. CONCURRENTLY
-- avoids blocking writes during build.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_task_usage_created_at
    ON task_usage (created_at);

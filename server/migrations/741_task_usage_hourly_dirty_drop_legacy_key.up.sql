-- Forward no-op: uq_task_usage_hourly_dirty_key is dropped atomically in 739 up
-- alongside the trigger switch so there is no intermediate constraint-conflict
-- window. This migration exists so the down direction can rebuild the legacy
-- 7-column unique index CONCURRENTLY (registered in concurrentDownIndexCleanups)
-- before 739 down attaches it via ADD CONSTRAINT ... UNIQUE USING INDEX.
SELECT 1;

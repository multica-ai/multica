-- Drop legacy 7-column unique constraint on task_usage_hourly_dirty now that
-- 739 has switched all dirty triggers to uq_task_usage_hourly_dirty_squad_key_idx.
-- Separated from 739/740 so the down migration can rebuild the backing index
-- CONCURRENTLY (registered in concurrentDownIndexCleanups) without write locks.
ALTER TABLE task_usage_hourly_dirty
    DROP CONSTRAINT IF EXISTS uq_task_usage_hourly_dirty_key;

-- Drop legacy 7-column unique constraint on task_usage_hourly now that 739 has
-- switched all upserts to uq_task_usage_hourly_squad_key_idx.
-- Separated from 739 so the down migration can rebuild the backing index
-- CONCURRENTLY (registered in concurrentDownIndexCleanups) without write locks.
ALTER TABLE task_usage_hourly
    DROP CONSTRAINT IF EXISTS uq_task_usage_hourly_key;

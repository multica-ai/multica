-- Forward no-op: uq_task_usage_hourly_squad_key_idx is dropped in 760 up
-- together with the pipeline switch. This migration exists so the down
-- direction can rebuild that 8-column unique index CONCURRENTLY (registered
-- in concurrentDownIndexCleanups) before 760 down restores the squad-only
-- pipeline that conflicts on it.
SELECT 1;

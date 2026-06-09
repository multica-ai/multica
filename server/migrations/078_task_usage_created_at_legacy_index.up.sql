-- Partial index supporting the rollup window function's legacy NULL
-- branch (072 added `updated_at` as nullable; rows that existed before
-- the column was added stay NULL until either backfill replaces them or
-- a subsequent UpsertTaskUsage refreshes them).
--
-- Built CONCURRENTLY because task_usage is a hot, large table — same
-- pattern as 074/075. Run this AFTER 077 is applied and BEFORE turning
-- on the read-path feature flag / scheduling pg_cron.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_task_usage_created_at_legacy
    ON task_usage (created_at)
    WHERE updated_at IS NULL;

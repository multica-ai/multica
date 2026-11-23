-- Add nullable squad_id dimension to task_usage_hourly and its dirty queue so
-- workspace usage rollups can aggregate and filter by triggering squad without
-- losing per-agent or per-project granularity.
--
-- Metadata-only ADD COLUMN (nullable, no default): instant DDL, no table
-- rewrite, no FK (matches agent_task_queue.squad_id from migration 127).
ALTER TABLE task_usage_hourly
    ADD COLUMN IF NOT EXISTS squad_id UUID;

ALTER TABLE task_usage_hourly_dirty
    ADD COLUMN IF NOT EXISTS squad_id UUID;

COMMENT ON COLUMN task_usage_hourly.squad_id IS
    'Triggering squad UUID (copied from agent_task_queue.squad_id). NULL for standalone non-squad runs and legacy pre-backfill buckets.';

-- Add a nullable autopilot_id dimension to task_usage_hourly and its dirty
-- queue so the Usage page can split tokens into manual and autopilot runs.
--
-- A run belongs to an autopilot when its task carries an autopilot run
-- (run-only autopilots) or when its issue was created by one
-- (issue.origin_type = 'autopilot'). The rollup derives it from those two
-- sources, so agent_task_queue needs no new column or backfill.
--
-- Metadata-only ADD COLUMN (nullable, no default): instant DDL, no rewrite.
ALTER TABLE task_usage_hourly
    ADD COLUMN IF NOT EXISTS autopilot_id UUID;

ALTER TABLE task_usage_hourly_dirty
    ADD COLUMN IF NOT EXISTS autopilot_id UUID;

COMMENT ON COLUMN task_usage_hourly.autopilot_id IS
    'Autopilot the run belongs to: autopilot_run.autopilot_id of the task, else issue.origin_id when the issue was created by an autopilot. NULL for manual runs and buckets rolled up before this column existed.';

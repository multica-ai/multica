-- Add nullable squad_id dimension to task_usage_hourly and its dirty queue, and
-- a dedicated usage_squad_id attribution column to agent_task_queue.
--
-- Keeping usage_squad_id separate from agent_task_queue.squad_id preserves the
-- existing leader/worker routing semantics of squad_id (workflow step ownership,
-- self-trigger suppression, conversation root ownership) while allowing
-- delegated member runs, squad-created sub-issues, wakeups, and quick-creates
-- to carry squad attribution cleanly. Rollup and dashboard queries read
-- COALESCE(atq.usage_squad_id, atq.squad_id) so existing rows require no
-- backfill on agent_task_queue.
--
-- Metadata-only ADD COLUMN (nullable, no default): instant DDL, no table
-- rewrite, no FK (matches agent_task_queue.squad_id from migration 127).
ALTER TABLE agent_task_queue
    ADD COLUMN IF NOT EXISTS usage_squad_id UUID;

ALTER TABLE task_usage_hourly
    ADD COLUMN IF NOT EXISTS squad_id UUID;

ALTER TABLE task_usage_hourly_dirty
    ADD COLUMN IF NOT EXISTS squad_id UUID;

COMMENT ON COLUMN agent_task_queue.usage_squad_id IS
    'Squad UUID for usage attribution and unified execution avatar grouping. Separate from squad_id (which carries leader/worker routing authority). NULL for standalone non-squad runs.';

COMMENT ON COLUMN task_usage_hourly.squad_id IS
    'Triggering squad UUID (from COALESCE(agent_task_queue.usage_squad_id, agent_task_queue.squad_id)). NULL for standalone non-squad runs and legacy pre-backfill buckets.';

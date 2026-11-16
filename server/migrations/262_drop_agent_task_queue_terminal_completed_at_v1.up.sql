-- Drop the superseded two-status partial index now that v2 is available.
--
-- Kept separate from migration 261 so both files contain exactly one
-- concurrent index statement and no rollout window is left without coverage.
DROP INDEX CONCURRENTLY IF EXISTS idx_agent_task_queue_terminal_completed_at;

-- Drop the superseded four-status partial index after v3 is available.
--
-- Kept separate from migration 254 so both files contain exactly one
-- concurrent index statement and no rollout window is left without coverage.
DROP INDEX CONCURRENTLY IF EXISTS idx_agent_task_queue_chat_pending_v2;

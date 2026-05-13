-- Drops agent_task_queue.last_heartbeat_at. The column was introduced in
-- migration 055 as scaffolding for "future enhancements" (telling stale
-- tasks apart from long-running ones), but no consumer was ever built:
-- runtime liveness is owned by agent_runtime.last_seen_at + the Redis
-- LivenessStore, and FailStaleTasks keys off dispatched_at/started_at.
-- The only writer was UpdateAgentTaskSession bumping it on every PinTaskSession
-- call, which was a wasted write. Drop the column so the write goes away too.

ALTER TABLE agent_task_queue
  DROP COLUMN IF EXISTS last_heartbeat_at;

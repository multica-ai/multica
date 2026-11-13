-- Single statement: CREATE INDEX CONCURRENTLY cannot run inside a transaction
-- or share a multi-command migration file.
--
-- Serves CountActiveTasksByRuntime, the runtime-wide concurrency cap check
-- (GitHub #9626) that runs on every claim for a runtime with a cap set. The
-- (runtime_id, id) index from migration 273 has no status column, so that
-- count had to read every historical task row on the runtime and filter by
-- status; cost grew with the runtime's history while the claim held the agent
-- row lock and the runtime claim lock. Partial on the capacity-bearing
-- statuses, so it only holds the few in-flight rows per runtime and stays
-- small no matter how much history accumulates.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_runtime_active
    ON agent_task_queue (runtime_id)
    WHERE status IN ('dispatched', 'running', 'waiting_local_directory');

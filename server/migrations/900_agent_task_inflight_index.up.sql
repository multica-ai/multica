-- The claim gate (spec 4.2) lists in-flight tasks per workspace. An agent with a long
-- queued backlog made that scan walk every queued row; this partial index holds only
-- the in-flight statuses.
CREATE INDEX CONCURRENTLY IF NOT EXISTS agent_task_queue_inflight_agent_idx ON agent_task_queue (agent_id) WHERE status IN ('dispatched', 'running', 'waiting_local_directory');

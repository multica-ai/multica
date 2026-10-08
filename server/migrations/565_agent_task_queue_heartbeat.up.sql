-- Per-task liveness (multica-ai/multica#9105).
--
-- Runtime liveness (agent_runtime.last_seen_at + the Redis LivenessStore)
-- answers "is the daemon alive?" and FailStaleTasks answers "has this task
-- run too long?" (dispatched_at/started_at). Neither answers "is this specific
-- running task still being polled by a live daemon?" -- a runtime can stay
-- online while one task on it has wedged. Migration 069 dropped an earlier
-- last_heartbeat_at because it had no consumer and its write was unthrottled;
-- this one is written at most once per minute per running task (see
-- TouchAgentTaskHeartbeat) and is read by ListStaleRunningAgentTasks.
ALTER TABLE agent_task_queue ADD COLUMN last_heartbeat_at TIMESTAMPTZ;

-- Only ever read for status='running' rows; partial index keeps it small.
CREATE INDEX idx_agent_task_queue_heartbeat
    ON agent_task_queue(last_heartbeat_at)
    WHERE status = 'running';

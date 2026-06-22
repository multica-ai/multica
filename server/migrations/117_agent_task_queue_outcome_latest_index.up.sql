-- Supports ListWorkspaceAgentTaskSnapshot's "latest completed/failed task per
-- agent" half. The endpoint reads atq.* rows, so the query first uses this
-- narrow index for the per-agent top-1 lookup and then fetches only those
-- latest rows instead of sorting every historical terminal task.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_agent_outcome_latest
    ON agent_task_queue (agent_id, completed_at DESC NULLS LAST)
    WHERE status IN ('completed', 'failed');

-- GetLastTaskSession queries by (agent_id, issue_id) with status = 'completed',
-- ordered by completed_at DESC. The existing (agent_id, status) index forces a
-- full scan of all tasks for the agent to find matching issue rows.
-- This partial index covers the lookup exactly and stays compact.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_session_lookup
    ON agent_task_queue (agent_id, issue_id, completed_at DESC)
    WHERE status = 'completed' AND session_id IS NOT NULL;

-- HasActiveTaskForIssue checks status IN ('queued', 'dispatched', 'running').
-- The partial index from migration 037 only covers queued/dispatched; the general
-- issue_id index (migration 035) covers all statuses but can't filter on status.
-- This partial index lets the hot-path comment trigger check skip completed/failed rows.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_issue_active
    ON agent_task_queue (issue_id)
    WHERE status IN ('queued', 'dispatched', 'running');

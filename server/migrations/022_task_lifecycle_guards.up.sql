-- Prevent duplicate pending tasks for the same issue (coalescing queue safety net).
-- At most one queued/dispatched task per issue at any time.
CREATE UNIQUE INDEX idx_one_pending_task_per_issue
    ON agent_task_queue (issue_id)
    WHERE status IN ('queued', 'dispatched');

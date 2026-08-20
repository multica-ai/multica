CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_branch_request_unique
    ON agent_task_queue (issue_id, branch_request_id)
    WHERE branch_request_id IS NOT NULL;

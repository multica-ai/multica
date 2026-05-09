-- Add a general index on agent_task_queue(issue_id) to support aggregation
-- queries like GetIssueUsageSummary that scan across all task statuses.
-- (Migration 022 only covers queued/dispatched rows via a partial index.)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_issue_id
    ON agent_task_queue (issue_id);

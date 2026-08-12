-- Drop the superseded queued/dispatched-only index after v2 is available.
DROP INDEX CONCURRENTLY IF EXISTS idx_one_pending_task_per_issue_agent;

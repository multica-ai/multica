-- Drop v2 only after migration 258's down step has restored v1.
DROP INDEX CONCURRENTLY IF EXISTS idx_one_pending_task_per_issue_agent_v2;

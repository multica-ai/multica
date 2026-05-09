-- Fix: the old index only allowed one pending task per issue across ALL agents.
-- This caused different agents' pending tasks to block each other.
-- Change to per-(issue, agent) so each agent can independently have one pending task.
DROP INDEX IF EXISTS idx_one_pending_task_per_issue;

CREATE UNIQUE INDEX idx_one_pending_task_per_issue_agent
    ON agent_task_queue (issue_id, agent_id)
    WHERE status IN ('queued', 'dispatched');

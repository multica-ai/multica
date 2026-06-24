-- Prevent duplicate agent runs from the same trigger comment.
-- The existing idx_one_pending_task_per_issue_agent dedup at (issue, agent)
-- does not guard against a trigger comment firing twice when the first task
-- has already transitioned past 'dispatched' (e.g. into 'running').
-- This index ensures at most one active task per (trigger_comment, agent) pair,
-- covering the full non-terminal lifecycle so that a second dispatch while
-- the first is still running is rejected at the database level.
CREATE UNIQUE INDEX idx_one_active_task_per_trigger_comment_agent
    ON agent_task_queue (trigger_comment_id, agent_id)
    WHERE status IN ('queued', 'dispatched', 'running', 'waiting_local_directory');

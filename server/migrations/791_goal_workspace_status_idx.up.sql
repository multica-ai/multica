-- The goals of a workspace by status and period: the brief, the planning cycle
-- and the pulse read the active ones.
CREATE INDEX CONCURRENTLY IF NOT EXISTS goal_workspace_status_idx ON goal (workspace_id, status, period_end);

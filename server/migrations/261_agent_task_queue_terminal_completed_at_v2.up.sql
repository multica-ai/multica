-- Single statement: CREATE INDEX CONCURRENTLY cannot run inside a transaction
-- or share a multi-command migration file.
--
-- Supersedes idx_agent_task_queue_terminal_completed_at (migration 231). The
-- run-time rollups now also count runs the user stopped mid-flight, so their
-- status filter reads `IN ('completed','failed','cancelled')`. A partial
-- index is only usable when the planner can prove the query's predicate
-- implies the index predicate, and the v1 two-status predicate cannot cover
-- a three-status filter — leaving those rollups on a full scan of a table
-- whose lifetime row count dwarfs any 30-day window.
--
-- Widening the predicate rather than dropping it keeps the index off the
-- queued / dispatched / running rows that the hot dispatch path churns: a
-- row still enters this index exactly once, when it reaches a terminal state.
--
-- The two failure rollups (ListDashboardFailuresDaily / ...ByAgent) keep the
-- two-status filter on purpose — a manual stop is not a failure and must not
-- dilute the error rate. They stay index-eligible regardless, since their
-- narrower predicate implies this wider one.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_terminal_completed_at_v2
    ON agent_task_queue (completed_at)
    WHERE status IN ('completed', 'failed', 'cancelled');

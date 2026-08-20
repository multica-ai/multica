-- Single statement: CREATE INDEX CONCURRENTLY cannot run inside a transaction
-- or share a multi-command migration file.
--
-- Every Usage-page dashboard rollup that reads agent_task_queue filters on
-- `status IN ('completed','failed') AND completed_at >= <cutoff>` and then
-- joins `agent` for the workspace scope — ListDashboardRunTimeDaily,
-- ListDashboardAgentRunTime, and the two failure rollups added for the
-- errors charts. agent_task_queue had no index on completed_at at all, so
-- each of those was a full scan of a table whose lifetime row count is
-- dominated by rows outside any 30-day window.
--
-- The partial predicate matches those queries' status filter verbatim so the
-- planner can prove index applicability, and it keeps the index off the
-- queued / dispatched / running rows that the hot dispatch path churns —
-- a row only enters this index once, when it reaches a terminal state.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_terminal_completed_at
    ON agent_task_queue (completed_at)
    WHERE status IN ('completed', 'failed');

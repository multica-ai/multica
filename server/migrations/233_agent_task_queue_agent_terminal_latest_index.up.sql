-- Single statement: CREATE INDEX CONCURRENTLY cannot run inside a transaction
-- or share a multi-command migration file.
--
-- ListWorkspaceAgentTaskSnapshot needs "the latest terminal task per agent".
-- Neither existing index can serve that shape:
--   - idx_agent_task_queue_agent (agent_id, status) from 001_init has no
--     completed_at, so the planner still has to read and sort every terminal
--     row of every agent in the workspace.
--   - idx_agent_task_queue_terminal_completed_at (completed_at) from 231 is
--     completed_at-first, built for the cross-agent Usage time-window
--     rollups, so it cannot skip to one agent's newest row.
-- Both stay: they still serve those other read patterns.
--
-- This index leads with agent_id and then carries the snapshot's full tie-break
-- order, so the per-agent lookup becomes an ordered index scan with LIMIT 1
-- instead of a workspace-wide sort. The partial predicate matches the query's
-- status filter verbatim so the planner can prove applicability, and it keeps
-- the index off the queued / dispatched / running rows the hot dispatch path
-- churns — a row only enters this index once, when it reaches a terminal state.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_agent_terminal_latest
    ON agent_task_queue (agent_id, completed_at DESC NULLS LAST, created_at DESC, id DESC)
    WHERE status IN ('completed', 'failed');

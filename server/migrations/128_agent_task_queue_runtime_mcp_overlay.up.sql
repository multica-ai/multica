-- Per-task MCP overlay computed at task enqueue (dispatch) time, layered on
-- top of agent.mcp_config when the daemon prepares the execution environment.
--
-- Stage 3 of the Composio epic (MUL-3721 / MUL-3715): the Composio integration
-- writes a fresh `{"mcpServers": {"composio": {"type": "http", "url": "...",
-- "headers": {"Authorization": "Bearer ..."}}}}` here for every task whose
-- initiator user has at least one active connection. The daemon claim
-- handler merges this on top of agent.mcp_config (overlay wins on name
-- collisions because it carries the live, user-scoped MCP session URL).
--
-- The value is short-lived: it carries a bearer token that becomes useless
-- once the task ends, so we wipe it on every terminal state transition via
-- the trigger below. Worst case a still-active task row keeps the token for
-- the duration of the run, mode-0600 in DB and never logged.
ALTER TABLE agent_task_queue
    ADD COLUMN runtime_mcp_overlay JSONB NULL;

COMMENT ON COLUMN agent_task_queue.runtime_mcp_overlay IS
    'Per-task MCP servers computed at dispatch time, merged on top of agent.mcp_config. Currently used by Composio integration to inject the initiator user''s session URL. Cleared after task completes via trg_clear_runtime_mcp_overlay.';

-- Auto-clear the overlay the moment the task enters any terminal state, so
-- the row in the queue never keeps a stale bearer beyond the live run. This
-- is a defense-in-depth measure on top of how short the Composio session
-- token already is — clearing here means a future audit of the table never
-- finds bearers attached to rows that finished hours/days ago.
--
-- Trigger-based rather than per-query SET clauses because the terminal
-- transitions live in ~12 different sqlc queries (CompleteAgentTask,
-- FailAgentTask, FailStaleTasks, ExpireStaleQueuedTasks, FailTasksFor
-- OfflineRuntimes, RecoverOrphanedTasksForRuntime, plus six Cancel*
-- variants); a trigger is a single source of truth that future queries
-- can't bypass.
CREATE OR REPLACE FUNCTION clear_runtime_mcp_overlay_on_terminal_state()
RETURNS TRIGGER AS $$
BEGIN
    -- Only act on actual transitions INTO a terminal state from a non-terminal
    -- one, and only when there is something to wipe. Re-touching an already-
    -- terminal row (or a row whose overlay is already NULL) leaves the
    -- column unchanged and the trigger is a cheap no-op.
    IF NEW.status IN ('completed', 'failed', 'cancelled')
       AND OLD.status IS DISTINCT FROM NEW.status
       AND NEW.runtime_mcp_overlay IS NOT NULL THEN
        NEW.runtime_mcp_overlay := NULL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_clear_runtime_mcp_overlay ON agent_task_queue;
CREATE TRIGGER trg_clear_runtime_mcp_overlay
    BEFORE UPDATE OF status ON agent_task_queue
    FOR EACH ROW
    EXECUTE FUNCTION clear_runtime_mcp_overlay_on_terminal_state();

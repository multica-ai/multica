-- MUL-4059: Add no-context runtime safeguard and max-inactivity timeout
-- to agent_task_queue. This migration:
--
--   1. Extends the agent_task_queue.status CHECK constraint with a new
--      'pending_context' value used by the no-context guard. Tasks that
--      fail the context guard land here instead of 'queued', so a daemon
--      claim never dispatches them. The companion sweeper
--      (sweepPendingContextTasks) revalidates them when the workspace
--      gains a repo / project directory and flips them back to 'queued'
--      or cancels them after a few attempts.
--
--   2. Adds context_guard JSONB column: a structured audit trail of the
--      guard decision (which (A) repos and (B) project resources were
--      seen, why the guard rejected the task). Stays NULL when the guard
--      was not consulted (legacy rows).
--
--   3. Adds context_guard_checked_at TIMESTAMPTZ: timestamps the most
--      recent guard evaluation. Lets the sweeper skip rows whose
--      evaluation is fresh (e.g. just checked 30s ago) and prioritise
--      rows whose context may have just changed.
--
--   4. Adds last_activity_at TIMESTAMPTZ + max_inactivity_secs INT:
--      powers the inactivity sweeper. last_activity_at is refreshed on
--      every daemon message / progress / session-pin / usage report;
--      max_inactivity_secs is resolved at enqueue time from the
--      task-level / agent-level / workspace-level / server-default
--      chain (NULL means "use server default"). The partial index keeps
--      the sweeper scan cheap as the row count grows.
--
-- ORDER MATTERS (P0 review fix): the backfill UPDATE on
-- `last_activity_at` MUST run after the ADD COLUMN. The original draft
-- placed it at the top of the file and the column did not exist yet,
-- which (a) aborts transactional migration runners and (b) silently
-- no-ops the backfill on non-transactional runners, leaving every
-- running task with `last_activity_at IS NULL` — the inactivity sweeper
-- then trips the cold-start branch and kills them on the first tick.
-- The block now lives at the end of the script, gated on
-- `last_activity_at IS NULL` so re-runs are idempotent.
ALTER TABLE agent_task_queue
    DROP CONSTRAINT agent_task_queue_status_check;

ALTER TABLE agent_task_queue
    ADD CONSTRAINT agent_task_queue_status_check
    CHECK (status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'pending_context', 'completed', 'failed', 'cancelled'));

ALTER TABLE agent_task_queue
    ADD COLUMN context_guard JSONB;

ALTER TABLE agent_task_queue
    ADD COLUMN context_guard_checked_at TIMESTAMPTZ;

ALTER TABLE agent_task_queue
    ADD COLUMN last_activity_at TIMESTAMPTZ;

ALTER TABLE agent_task_queue
    ADD COLUMN max_inactivity_secs INT;

-- Partial index that powers sweepInactiveTasks: only running rows are
-- candidates, and we always compare last_activity_at against now(). A
-- non-partial index on (status, last_activity_at) would include the
-- 100k+ terminal rows and double the index size with no benefit.
CREATE INDEX idx_agent_task_queue_running_activity
    ON agent_task_queue (last_activity_at)
    WHERE status = 'running';

-- Partial index that powers sweepPendingContextTasks and the daemon
-- claim path's "skip pending_context" lookup. Together with the new
-- CHECK constraint value this lets the sweeper do a single index scan
-- for all pending-context work without touching the (much larger)
-- queued queue.
CREATE INDEX idx_agent_task_queue_pending_context
    ON agent_task_queue (context_guard_checked_at)
    WHERE status = 'pending_context';

-- Backfill (P0-1 review fix). Runs LAST, after the ADD COLUMNs, so the
-- column actually exists at UPDATE time. The IS NULL guard makes the
-- statement idempotent — re-runs after a partial success (e.g. a
-- migration runner that interrupted after the column was added but
-- before this backfill committed) leave rows that already have a
-- timestamp alone.
--
-- A running task that has been alive for longer than 2.5h
-- (runningTimeoutSeconds) is already in the cold-start window the
-- existing dispatchTimeout / runningTimeout sweeper handles; the
-- inactivity sweeper treats started_at as activity so it never
-- duplicates that decision. See P0-1 in the issue thread for the
-- upgrade-incident this guard prevents.
UPDATE agent_task_queue
SET last_activity_at = COALESCE(started_at, created_at)
WHERE status IN ('running', 'dispatched', 'waiting_local_directory')
  AND last_activity_at IS NULL;
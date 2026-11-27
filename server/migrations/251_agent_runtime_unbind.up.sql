-- Migration 251 (MUL-5559): deleting a runtime unbinds its agents instead of
-- destroying them.
--
-- Before this migration an agent physically could not exist without a runtime
-- (agent.runtime_id NOT NULL), so the runtime-delete flow had no third option:
-- it archived the runtime's agents and then hard-deleted the rows, taking their
-- chat sessions with them. The confirmation dialog called that "archive", which
-- is not what the user got — the agents and every conversation with them were
-- gone. Retiring a laptop is an ordinary action; losing the agents configured
-- on it is not an ordinary consequence.
--
-- Two columns become nullable, not one:
--
--   1. agent.runtime_id — NULL now means "unbound": the agent, its
--      instructions, skills, chats, labels, channel installations and
--      autopilot config all survive, it simply cannot run until it is bound to
--      a runtime again. Unbound is orthogonal to archived.
--      AgentReadiness already refuses an agent with no runtime, so the
--      scheduling safety gate needs no change.
--
--   2. agent_task_queue.runtime_id — without this, unbinding the agent would
--      still lose its whole task history: that column is NOT NULL with an
--      ON DELETE CASCADE FK, which cascades further into task_message,
--      task_usage and task_token. ReassignTasksToRuntime already documents the
--      same trap for the legacy-daemon merge path ("Required before deleting
--      the old runtime row because agent_task_queue has an ON DELETE CASCADE
--      FK that would otherwise drop historical tasks"). The delete transaction
--      now nulls these rows explicitly, so the cascade never fires.
--
-- No new foreign keys or cascading actions are introduced (repository rule):
-- the unbind itself is an explicit application-layer UPDATE inside the same
-- transaction as the runtime delete. The pre-existing CASCADE on
-- agent_task_queue.runtime_id is left in place but is now inert on this path —
-- it stays as the last-resort guarantee that no row can reference a runtime
-- that no longer exists.
--
-- The CHECK is the invariant that keeps NULL confined to history: an ACTIVE
-- task must always have a runtime, so claim / dispatch / delivery-CAS paths can
-- never observe runtime_id IS NULL. It is expressed with completed_at rather
-- than a status list because every terminal transition stamps completed_at
-- (CompleteAgentTask / FailAgentTask / the five cancel queries), and because
-- naming enumerable status values in a constraint would force a migration for
-- each new status. A future non-terminal status therefore fails closed — it
-- must carry a runtime — instead of silently slipping through.
--
-- NOT VALID keeps this a metadata-only add on a hot table (no ACCESS EXCLUSIVE
-- full-table scan); PostgreSQL still enforces it on every INSERT and UPDATE
-- from this point on, which is exactly what the invariant needs. Existing rows
-- all satisfy it (runtime_id was NOT NULL until now), so there is nothing to
-- back-fill; a later migration can VALIDATE it during a quiet window.

ALTER TABLE agent
    ALTER COLUMN runtime_id DROP NOT NULL;

ALTER TABLE agent_task_queue
    ALTER COLUMN runtime_id DROP NOT NULL;

ALTER TABLE agent_task_queue
    ADD CONSTRAINT agent_task_queue_active_requires_runtime
    CHECK (runtime_id IS NOT NULL OR completed_at IS NOT NULL)
    NOT VALID;

-- Runtime deletion pauses affected automations so a schedule cannot append one
-- identical skipped run on every tick forever. The reason is persisted
-- separately from status: clients can explain the recoverable condition, and a
-- user can rebind the agent then resume the unchanged automation.
ALTER TABLE autopilot
    ADD COLUMN pause_reason TEXT;

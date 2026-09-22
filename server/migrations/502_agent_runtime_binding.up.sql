-- Migration 501 (SE-37711 / SE-37664): an agent may be bound to an ordered
-- pool of runtimes, not a single one.
--
-- Until now an agent ran on exactly one runtime (agent.runtime_id). When that
-- runtime's provider hit a quota wall the agent simply stopped: there was no
-- second place to send the work. This table records an ordered list of
-- runtimes per agent so the dispatcher can fall through from the first choice
-- to the next live one instead of stalling.
--
-- Shape and invariants:
--   * priority orders the pool, 0 first. Lower runs before higher.
--   * (agent_id, runtime_id) is unique — a runtime appears in an agent's pool
--     at most once (see the concurrent unique index that follows).
--   * (agent_id, priority) is unique — the order is total and deterministic,
--     never "two runtimes tied for second" (its own concurrent index).
--   * workspace_id is carried on the row so resolution and teardown filter by
--     workspace without joining back to agent; assignees/rows in this codebase
--     are workspace-scoped by convention, not by FK.
--
-- No foreign keys or cascades (repository rule): agent and agent_runtime
-- lifetimes are managed in application code. Deleting a runtime removes its
-- rows here and promotes the next binding inside the same transaction as the
-- runtime delete (runtime_teardown); deleting an agent removes its rows the
-- same way. The legacy agent.runtime_id column is retained and kept in sync as
-- the priority-0 projection of this pool, so older read paths and installed
-- clients keep working unchanged.
CREATE TABLE IF NOT EXISTS agent_runtime_binding (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    runtime_id UUID NOT NULL,
    priority INTEGER NOT NULL DEFAULT 0 CHECK (priority >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

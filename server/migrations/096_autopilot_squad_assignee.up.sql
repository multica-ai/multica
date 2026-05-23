-- Autopilot: support assigning to a squad (MUL-2429).
--
-- Path A "Squad-as-Leader": when an autopilot's assignee is a squad, dispatch
-- still resolves to a single agent (squad.leader_id) — same semantics as a
-- human manually assigning an issue to that squad. We model this by adding an
-- assignee_type column and dropping the hard FK on assignee_id so the same
-- UUID column can reference either agent(id) or squad(id) depending on the
-- type. Referential integrity is enforced in the application layer (handler
-- validates the squad/agent is in the workspace; dispatch re-resolves at run
-- time and skip-records doomed runs instead of crashing).

ALTER TABLE autopilot
    DROP CONSTRAINT IF EXISTS autopilot_assignee_id_fkey;

ALTER TABLE autopilot
    ADD COLUMN assignee_type TEXT NOT NULL DEFAULT 'agent'
        CHECK (assignee_type IN ('agent', 'squad'));

-- Composite index lets lookups discriminate by type cheaply, e.g. "all
-- autopilots whose assignee is squad X" without scanning the whole table.
-- The legacy idx_autopilot_assignee(assignee_id) stays for plain id lookups.
CREATE INDEX IF NOT EXISTS idx_autopilot_assignee_type_id
    ON autopilot (assignee_type, assignee_id);

-- autopilot_run.squad_id: attribution hook. Populated when ap.assignee_type =
-- 'squad' so reports can group runs by squad even though the executing agent
-- (and the cost it accrues) is the leader. First version does not consume
-- the column; it exists so we never need a backfill.
ALTER TABLE autopilot_run
    ADD COLUMN squad_id UUID REFERENCES squad(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_autopilot_run_squad_id
    ON autopilot_run (squad_id) WHERE squad_id IS NOT NULL;

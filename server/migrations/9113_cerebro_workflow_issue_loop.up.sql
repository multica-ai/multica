-- FIR-2283 "Issue workflow" type. Adds the columns a cerebro_workflow row
-- needs to be either a standard rule (unchanged) or an issue_loop recipe:
--
--   workflow_type              'standard' (default, unchanged behavior) or
--                               'issue_loop' — a saved Plan/Build/Delivery-gate
--                               recipe designed on the Issue workflow surface.
--   loop_spec                  the recipe JSON (goal, definition_of_done,
--                               verification[], caps, planning) for an
--                               issue_loop row. Null for standard rows.
--   generated_from_workflow_id when set, this row is one of the
--                               dispatch/gate/escalate rules loops.Compile
--                               materialized from an issue_loop recipe — the
--                               bridge's output, not something a person edits
--                               directly. List hides these; deleting the
--                               parent cascades to them.
--
-- These three columns are read/written directly against the pool (see
-- workflows/issue_loop_columns.go), the same "raw SQL against
-- cerebro_workflow" pattern the loops package already uses for its own
-- tables (server/internal/cerebro/loops/store.go) — so the new columns work
-- without a sqlc regeneration pass over the rest of this package's queries.
ALTER TABLE cerebro_workflow
    ADD COLUMN IF NOT EXISTS workflow_type TEXT NOT NULL DEFAULT 'standard',
    ADD COLUMN IF NOT EXISTS loop_spec JSONB,
    ADD COLUMN IF NOT EXISTS generated_from_workflow_id UUID REFERENCES cerebro_workflow(id) ON DELETE CASCADE;

ALTER TABLE cerebro_workflow
    DROP CONSTRAINT IF EXISTS cerebro_workflow_workflow_type_known;

ALTER TABLE cerebro_workflow
    ADD CONSTRAINT cerebro_workflow_workflow_type_known
        CHECK (workflow_type IN ('standard', 'issue_loop'));

CREATE INDEX IF NOT EXISTS idx_cerebro_workflow_generated_from
    ON cerebro_workflow (generated_from_workflow_id)
    WHERE generated_from_workflow_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_cerebro_workflow_workflow_type
    ON cerebro_workflow (workspace_id, workflow_type)
    WHERE workflow_type <> 'standard';

-- FIR-1659 Fase 2: personal saved filters for the issue list/board/swimlane views.
-- A saved filter is a named snapshot of the issue view-store filter state,
-- owned by the member who created it and scoped to a single workspace. The
-- snapshot itself is opaque JSON (the frontend view-store shape) so new filter
-- types need no schema change here.
--
-- Personal filters only in this migration. Sharing (saved_filter_shares,
-- Fase 3) and the "may create shared filters" group capability (Fase 4) land
-- in later 9000-series cerebro migrations.

CREATE TABLE IF NOT EXISTS cerebro_saved_filters (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    owner_id      UUID NOT NULL,
    name          TEXT NOT NULL CHECK (length(btrim(name)) > 0),
    surface       TEXT NOT NULL DEFAULT 'issues',
    filter_state  JSONB NOT NULL DEFAULT '{}'::jsonb,
    position      INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_saved_filters_owner
    ON cerebro_saved_filters (workspace_id, owner_id);

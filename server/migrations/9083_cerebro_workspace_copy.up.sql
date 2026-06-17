-- 9083_cerebro_workspace_copy: non-destructive copy of workspace contents into
-- another workspace (TECH-3582). Copies create NEW rows in the target workspace
-- and never delete or mutate the source, so the source workspace stays a full
-- read-only safety net / archive.
--
-- Two schema changes:
--   1. agent.runtime_id becomes nullable, so an agent can be copied into the
--      target workspace "parked" (no runtime) until its owner re-pairs a runtime
--      there via the guide. Today runtime_id is NOT NULL, which would block the
--      copy. CASCADE/RESTRICT FK behaviour is unchanged.
--   2. cerebro_workspace_copy_map records, per copy run, the mapping from a
--      source entity to the new target entity. It powers (a) idempotency
--      (re-running a copy does not duplicate), (b) internal-reference rewriting
--      (a copied issue's links resolve to the copied targets), and (c) the
--      "moved/forwarding" pointer left on the source for old bookmarks.

-- CEREBRO-PATCH(workspace-copy): make agent.runtime_id nullable for parked copies.
DO $$ BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'agent' AND column_name = 'runtime_id' AND is_nullable = 'NO'
    ) THEN
        ALTER TABLE agent ALTER COLUMN runtime_id DROP NOT NULL;
    END IF;
END $$;

-- CEREBRO-PATCH(workspace-copy): per-run source->target id mapping.
CREATE TABLE IF NOT EXISTS cerebro_workspace_copy_map (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id               UUID NOT NULL,
    source_workspace_id  UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    target_workspace_id  UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    entity_type          TEXT NOT NULL,
    source_id            UUID NOT NULL,
    target_id            UUID NOT NULL,
    source_number        INT,
    target_number        INT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- idempotency: a given source entity maps to at most one target per target workspace
    CONSTRAINT uq_copy_map_target UNIQUE (target_workspace_id, entity_type, source_id)
);

CREATE INDEX IF NOT EXISTS idx_copy_map_run ON cerebro_workspace_copy_map (run_id);
CREATE INDEX IF NOT EXISTS idx_copy_map_source ON cerebro_workspace_copy_map (source_workspace_id, entity_type, source_id);

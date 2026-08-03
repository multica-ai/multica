-- FIR-4220: add generation metadata without changing active legacy mandates.

ALTER TABLE cerebro_task_mandate
    ADD COLUMN IF NOT EXISTS claim_generation BIGINT NOT NULL DEFAULT 1
        CHECK (claim_generation > 0),
    ADD COLUMN IF NOT EXISTS producer TEXT,
    ADD COLUMN IF NOT EXISTS finalizer TEXT,
    ADD COLUMN IF NOT EXISTS lifecycle_state TEXT NOT NULL DEFAULT 'legacy'
        CHECK (lifecycle_state IN ('legacy', 'draft', 'finalized')),
    ADD COLUMN IF NOT EXISTS inventory_version TEXT,
    ADD COLUMN IF NOT EXISTS discovery_version TEXT,
    ADD COLUMN IF NOT EXISTS finalized_grant_digest TEXT;

ALTER TABLE cerebro_task_mandate
    DROP COLUMN IF EXISTS finalized_grant_digest,
    DROP COLUMN IF EXISTS discovery_version,
    DROP COLUMN IF EXISTS inventory_version,
    DROP COLUMN IF EXISTS lifecycle_state,
    DROP COLUMN IF EXISTS finalizer,
    DROP COLUMN IF EXISTS producer,
    DROP COLUMN IF EXISTS claim_generation;

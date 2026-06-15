ALTER TABLE cerebro_note
    DROP COLUMN IF EXISTS locked_by,
    DROP COLUMN IF EXISTS locked_at,
    DROP COLUMN IF EXISTS lock_heartbeat_at;

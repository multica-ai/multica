DROP INDEX IF EXISTS idx_cerebro_approval_request_consumable;

ALTER TABLE cerebro_approval_request
    DROP COLUMN IF EXISTS consumed_at,
    DROP COLUMN IF EXISTS single_use;

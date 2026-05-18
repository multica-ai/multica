DROP INDEX IF EXISTS idx_autopilot_private_owner;

ALTER TABLE autopilot
    DROP COLUMN IF EXISTS is_private;

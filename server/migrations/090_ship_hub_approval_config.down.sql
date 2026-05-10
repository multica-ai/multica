-- Reverse of 090_ship_hub_approval_config.up.sql.
DROP INDEX IF EXISTS idx_ship_release_signoff_release;
DROP TABLE IF EXISTS ship_release_signoff;

ALTER TABLE workspace DROP COLUMN IF EXISTS ship_hub_approver_can_be_author;

ALTER TABLE workspace DROP CONSTRAINT IF EXISTS ship_hub_approval_critical_check;
ALTER TABLE workspace DROP CONSTRAINT IF EXISTS ship_hub_approval_high_check;
ALTER TABLE workspace DROP CONSTRAINT IF EXISTS ship_hub_approval_medium_check;
ALTER TABLE workspace DROP CONSTRAINT IF EXISTS ship_hub_approval_low_check;

ALTER TABLE workspace DROP COLUMN IF EXISTS ship_hub_approval_critical;
ALTER TABLE workspace DROP COLUMN IF EXISTS ship_hub_approval_high;
ALTER TABLE workspace DROP COLUMN IF EXISTS ship_hub_approval_medium;
ALTER TABLE workspace DROP COLUMN IF EXISTS ship_hub_approval_low;

-- Reverse 9057: drop workspace-level overrides (the sentinel-user rows) and
-- the locked column. Per-user rows are untouched.
DELETE FROM cerebro_feature_flags
    WHERE user_id = '00000000-0000-0000-0000-000000000000';

ALTER TABLE cerebro_feature_flags
    DROP COLUMN IF EXISTS locked;

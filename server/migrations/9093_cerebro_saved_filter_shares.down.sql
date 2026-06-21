-- Rollback for 9093_cerebro_saved_filter_shares.up.sql.
DROP TABLE IF EXISTS cerebro_saved_filter_shares;

ALTER TABLE cerebro_saved_filters
    DROP COLUMN IF EXISTS visibility;

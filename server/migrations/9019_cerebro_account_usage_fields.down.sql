ALTER TABLE cerebro_account
    DROP CONSTRAINT IF EXISTS cerebro_account_usage_window_pct_range;
ALTER TABLE cerebro_account
    DROP COLUMN IF EXISTS paused_manual,
    DROP COLUMN IF EXISTS extra_spend_on,
    DROP COLUMN IF EXISTS throttled_until,
    DROP COLUMN IF EXISTS usage_window_pct;

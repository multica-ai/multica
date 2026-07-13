ALTER TABLE cerebro_account
    DROP CONSTRAINT IF EXISTS cerebro_account_usage_5h_pct_range;
ALTER TABLE cerebro_account
    DROP CONSTRAINT IF EXISTS cerebro_account_usage_7d_pct_range;
ALTER TABLE cerebro_account
    DROP COLUMN IF EXISTS usage_5h_pct,
    DROP COLUMN IF EXISTS usage_5h_resets_at,
    DROP COLUMN IF EXISTS usage_7d_pct,
    DROP COLUMN IF EXISTS usage_7d_resets_at;

-- FIR-3118: provider-reported rolling usage windows on cerebro_account.
-- The daemon fetches Claude's OAuth usage endpoint after each task run and
-- reports exact window utilization, replacing the log-scraped
-- usage_window_pct approximation for providers that expose it.
--   usage_5h_pct       — % of the 5-hour window consumed (0..100).
--   usage_5h_resets_at — when the 5-hour window resets.
--   usage_7d_pct       — % of the 7-day (weekly) window consumed (0..100).
--   usage_7d_resets_at — when the 7-day window resets.
ALTER TABLE cerebro_account
    ADD COLUMN IF NOT EXISTS usage_5h_pct       REAL,
    ADD COLUMN IF NOT EXISTS usage_5h_resets_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS usage_7d_pct       REAL,
    ADD COLUMN IF NOT EXISTS usage_7d_resets_at TIMESTAMPTZ;

ALTER TABLE cerebro_account
    DROP CONSTRAINT IF EXISTS cerebro_account_usage_5h_pct_range;
ALTER TABLE cerebro_account
    ADD CONSTRAINT cerebro_account_usage_5h_pct_range
        CHECK (usage_5h_pct IS NULL OR (usage_5h_pct >= 0 AND usage_5h_pct <= 100));

ALTER TABLE cerebro_account
    DROP CONSTRAINT IF EXISTS cerebro_account_usage_7d_pct_range;
ALTER TABLE cerebro_account
    ADD CONSTRAINT cerebro_account_usage_7d_pct_range
        CHECK (usage_7d_pct IS NULL OR (usage_7d_pct >= 0 AND usage_7d_pct <= 100));

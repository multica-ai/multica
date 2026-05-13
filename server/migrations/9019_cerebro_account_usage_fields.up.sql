-- JEH-998: extend cerebro_account with the four operational fields the
-- coordinator agent needs to assess "who is available?".
--   usage_window_pct  — % of current usage window consumed (0..100).
--   throttled_until   — server-reported 429 cooldown end.
--   extra_spend_on    — user enabled "allow extra spend" (Claude pro/max).
--   paused_manual     — user manually paused this account from the UI.
ALTER TABLE cerebro_account
    ADD COLUMN IF NOT EXISTS usage_window_pct REAL,
    ADD COLUMN IF NOT EXISTS throttled_until  TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS extra_spend_on   BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS paused_manual    BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE cerebro_account
    DROP CONSTRAINT IF EXISTS cerebro_account_usage_window_pct_range;
ALTER TABLE cerebro_account
    ADD CONSTRAINT cerebro_account_usage_window_pct_range
        CHECK (usage_window_pct IS NULL OR (usage_window_pct >= 0 AND usage_window_pct <= 100));

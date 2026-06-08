-- FIR-40: display currency for cost (USD -> DKK/EUR).
--
-- Cost is always stored in USD cents. Currency is a pure display concern: we
-- convert at render time with a cached daily rate, so historical cost numbers
-- are never bound to a particular day's rate and the display currency can
-- change with no data migration.
--
-- This table is the cached rate. It is global (not per-workspace) — a USD->DKK
-- rate is the same for every tenant. A daily runs.firtal.com job upserts one
-- row per (base, target) pair from ECB reference rates (Frankfurter). Display
-- reads the latest row; if today's fetch fails, the last known row stands and
-- `fetched_at` exposes the staleness.
--
-- Cerebro 9xxx namespace + cerebro_ table prefix so the upstream schema stays
-- untouched. Idempotent (CREATE IF NOT EXISTS) so the migration test re-run is
-- a no-op.

CREATE TABLE IF NOT EXISTS cerebro_exchange_rates (
    base_currency   TEXT          NOT NULL,
    target_currency TEXT          NOT NULL,
    -- Target-currency units per 1 base unit. NUMERIC keeps full forex precision
    -- (no float drift); 8 fractional digits is standard for reference rates.
    rate            NUMERIC(18, 8) NOT NULL CHECK (rate > 0),
    -- When the upstream source published / we fetched this rate.
    fetched_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    -- When this row was last written by the fetch job.
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    PRIMARY KEY (base_currency, target_currency)
);

-- Freshness lookups ("is the rate stale?") scan by fetched_at.
CREATE INDEX IF NOT EXISTS idx_cerebro_exchange_rates_fetched_at
    ON cerebro_exchange_rates (fetched_at DESC);

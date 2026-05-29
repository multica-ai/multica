-- 9044_cerebro_cost_optimization_measurement: one cost-saving measurement per
-- agent run (FIR-2325 phase 4).
--
-- The runtime records exactly one row per (task, saving_key) for every saving
-- that is NOT off for the run's workspace:
--   - shadow — the run executed unchanged; baseline_value/effective_value/
--              saved_cents capture what the saving WOULD have saved.
--   - on     — the saving was applied during the run; the same columns capture
--              what it ACTUALLY saved against the no-saving baseline.
-- A saving in 'off' mode writes no row, so this table only ever holds savings
-- that someone chose to measure.
--
-- Values are stored in the saving's own metric unit (CostSavingMetric in
-- packages/cerebro-cost-optimization/registry.ts): platform_calls, input_tokens,
-- model_cost (US cents), or context_tokens. saved_cents is the monetary saving
-- when the metric is directly priced (model_cost); for the other metrics it is
-- 0 here and converted to kroner by the phase-5 dashboard, which knows the
-- price-per-unit. Keeping the raw measured value plus an optional cents lets the
-- dashboard show estimated-would-save vs measured-actually-save without this
-- table guessing a price.
--
-- UNIQUE(task_id, saving_key) makes the recorder idempotent: a retried run
-- overwrites its own measurement instead of double-counting.

CREATE TABLE IF NOT EXISTS cerebro_cost_optimization_measurement (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    task_id UUID NOT NULL,
    saving_key TEXT NOT NULL,
    mode TEXT NOT NULL,
    applied BOOLEAN NOT NULL,
    metric TEXT NOT NULL,
    baseline_value BIGINT NOT NULL,
    effective_value BIGINT NOT NULL,
    saved_cents BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_cost_optimization_measurement_mode_known
        CHECK (mode IN ('shadow', 'on')),
    CONSTRAINT cerebro_cost_optimization_measurement_unique_per_run
        UNIQUE (task_id, saving_key)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_cost_optimization_measurement_dashboard
    ON cerebro_cost_optimization_measurement (workspace_id, saving_key, created_at);

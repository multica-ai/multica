-- name: RecordCerebroCostOptimizationMeasurement :exec
-- Idempotent per run: a retried task overwrites its own measurement rather than
-- inserting a duplicate (UNIQUE on task_id + saving_key).
INSERT INTO cerebro_cost_optimization_measurement (
    workspace_id, task_id, saving_key, mode, applied, held_out, metric,
    baseline_value, effective_value, saved_cents, actual_cost_cents, detail_json
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (task_id, saving_key) DO UPDATE
SET mode = EXCLUDED.mode,
    applied = EXCLUDED.applied,
    held_out = EXCLUDED.held_out,
    metric = EXCLUDED.metric,
    baseline_value = EXCLUDED.baseline_value,
    effective_value = EXCLUDED.effective_value,
    saved_cents = EXCLUDED.saved_cents,
    actual_cost_cents = EXCLUDED.actual_cost_cents,
    detail_json = EXCLUDED.detail_json,
    created_at = now();

-- name: DashboardCerebroCostOptimization :many
-- Per-saving rollup split by measurement arm for the phase-5 dashboard. The arm
-- is (mode, held_out): shadow runs, "on" runs where the saving was applied
-- (treatment), and "on" runs deliberately held out (control). The Go handler
-- turns these rows into the two dashboard numbers per saving:
--   estimated would-save  = sum(baseline_value - effective_value) across shadow
--                           + treatment + control rows (the per-run hypothetical)
--   measured actually-saved = control avg actual cost - treatment avg actual cost
-- run_count and the actual-cost sum let the handler average each arm.
SELECT
    saving_key,
    mode,
    held_out,
    metric,
    COUNT(*) AS run_count,
    COALESCE(SUM(baseline_value - effective_value), 0)::BIGINT AS total_saved_units,
    COALESCE(SUM(saved_cents), 0)::BIGINT AS total_saved_cents,
    COALESCE(SUM(actual_cost_cents), 0)::BIGINT AS total_actual_cost_cents
FROM cerebro_cost_optimization_measurement
WHERE workspace_id = $1
GROUP BY saving_key, mode, held_out, metric;

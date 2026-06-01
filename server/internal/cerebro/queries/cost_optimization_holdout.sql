-- name: ListCerebroCostOptimizationHoldout :many
SELECT saving_key, holdout_pct FROM cerebro_cost_optimization_holdout
WHERE workspace_id = $1;

-- name: GetCerebroCostOptimizationHoldout :one
SELECT holdout_pct FROM cerebro_cost_optimization_holdout
WHERE workspace_id = $1 AND saving_key = $2;

-- name: UpsertCerebroCostOptimizationHoldout :exec
INSERT INTO cerebro_cost_optimization_holdout (workspace_id, saving_key, holdout_pct, updated_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (workspace_id, saving_key) DO UPDATE
SET holdout_pct = EXCLUDED.holdout_pct, updated_by = EXCLUDED.updated_by, updated_at = NOW();

-- name: DeleteCerebroCostOptimizationHoldout :exec
DELETE FROM cerebro_cost_optimization_holdout
WHERE workspace_id = $1 AND saving_key = $2;

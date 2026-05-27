-- name: ListCerebroCostOptimization :many
SELECT saving_key, mode FROM cerebro_cost_optimization
WHERE workspace_id = $1;

-- name: UpsertCerebroCostOptimization :exec
INSERT INTO cerebro_cost_optimization (workspace_id, saving_key, mode, updated_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (workspace_id, saving_key) DO UPDATE
SET mode = EXCLUDED.mode, updated_by = EXCLUDED.updated_by, updated_at = NOW();

-- name: DeleteCerebroCostOptimization :exec
DELETE FROM cerebro_cost_optimization
WHERE workspace_id = $1 AND saving_key = $2;

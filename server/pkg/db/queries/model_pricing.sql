-- name: GetModelPricingCatalog :one
SELECT (document -> 'catalog')::jsonb AS catalog, checked_at, succeeded_at, last_error FROM model_pricing_catalog WHERE id = TRUE;

-- name: GetModelPricingSyncState :one
SELECT * FROM model_pricing_catalog WHERE id = TRUE;

-- name: TryLockModelPricingSync :one
SELECT pg_try_advisory_xact_lock(hashtextextended('model_pricing_sync', 0));

-- name: SaveModelPricingCatalog :exec
INSERT INTO model_pricing_catalog (id, document, checked_at, succeeded_at, last_error)
VALUES (TRUE, @document, now(), now(), '')
ON CONFLICT (id) DO UPDATE SET document = EXCLUDED.document, checked_at = now(), succeeded_at = now(), last_error = '';

-- name: RecordModelPricingFailure :exec
INSERT INTO model_pricing_catalog (id, last_error) VALUES (TRUE, @last_error)
ON CONFLICT (id) DO UPDATE SET checked_at = now(), last_error = EXCLUDED.last_error;

-- name: GetWorkspaceModelPricing :one
SELECT * FROM workspace_model_pricing WHERE workspace_id = @workspace_id;

-- name: SaveWorkspaceModelPricing :one
INSERT INTO workspace_model_pricing (workspace_id, overrides, revision, updated_by)
VALUES (@workspace_id, @overrides, @revision, @updated_by)
ON CONFLICT (workspace_id) DO UPDATE SET overrides = EXCLUDED.overrides,
    revision = EXCLUDED.revision, updated_by = EXCLUDED.updated_by, updated_at = now()
RETURNING *;

-- name: LockWorkspaceForModelPricing :one
SELECT id FROM workspace WHERE id = @workspace_id FOR UPDATE;

-- name: DeleteWorkspaceModelPricing :exec
DELETE FROM workspace_model_pricing WHERE workspace_id = @workspace_id;

-- name: ListModelPricingWorkspaceIDs :many
SELECT id FROM workspace;

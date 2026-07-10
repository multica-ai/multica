-- Model registry (FIR-2698): single-source model metadata (label, provider,
-- context window, list prices) with propose → review → approve versioning.
-- Schema lives in 9120_cerebro_model_registry.{up,down}.sql. The registry is a
-- deployment-wide singleton addressed by registry_key = 'default'.

-- name: GetModelRegistry :one
SELECT * FROM model_registry
WHERE registry_key = 'default';

-- GetModelRegistryForUpdate locks the singleton row so concurrent merges
-- serialize on it.
-- name: GetModelRegistryForUpdate :one
SELECT * FROM model_registry
WHERE registry_key = 'default'
FOR UPDATE;

-- name: ApplyModelRegistrySnapshot :one
UPDATE model_registry SET
    snapshot        = $1,
    current_version = $2,
    updated_at      = now()
WHERE registry_key = 'default'
RETURNING *;

-- name: UpdateModelRegistryOwnership :one
UPDATE model_registry SET
    owner_id     = $1,
    approver_ids = $2,
    updated_at   = now()
WHERE registry_key = 'default'
RETURNING *;

-- --- Version snapshots (append-only) ---

-- name: ListModelRegistryVersions :many
SELECT * FROM model_registry_version
WHERE registry_id = $1
ORDER BY created_at DESC;

-- name: GetModelRegistryVersion :one
SELECT * FROM model_registry_version
WHERE registry_id = $1 AND version = $2;

-- name: CreateModelRegistryVersion :one
INSERT INTO model_registry_version (
    registry_id, version, snapshot, description, created_by
)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- --- Change requests ---

-- name: ListModelRegistryChangeRequests :many
SELECT * FROM model_registry_change_request
WHERE registry_id = $1
ORDER BY created_at DESC;

-- name: ListPendingModelRegistryChangeRequests :many
SELECT * FROM model_registry_change_request
WHERE registry_id = $1 AND status = 'pending'
ORDER BY created_at DESC;

-- name: GetModelRegistryChangeRequest :one
SELECT * FROM model_registry_change_request
WHERE id = $1;

-- GetModelRegistryChangeRequestForUpdate locks the row so concurrent reviews
-- on the same change request can't race past the status check.
-- name: GetModelRegistryChangeRequestForUpdate :one
SELECT * FROM model_registry_change_request
WHERE id = $1
FOR UPDATE;

-- name: CreateModelRegistryChangeRequest :one
INSERT INTO model_registry_change_request (
    registry_id, title, description, base_version, proposed_version,
    proposed_snapshot, proposed_by, work_session_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ReviewModelRegistryChangeRequest :one
UPDATE model_registry_change_request SET
    status         = $2,
    reviewed_by    = $3,
    reviewed_at    = now(),
    review_comment = $4,
    updated_at     = now()
WHERE id = $1
RETURNING *;

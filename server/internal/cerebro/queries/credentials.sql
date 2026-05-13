-- name: CreateCerebroCredential :one
INSERT INTO cerebro_credential (
    workspace_id, type, name, description, encrypted_value, value_hint,
    metadata, expires_at, created_by_type, created_by_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetCerebroCredential :one
SELECT * FROM cerebro_credential WHERE id = $1;

-- name: ListCerebroCredentials :many
SELECT * FROM cerebro_credential
WHERE workspace_id = $1
ORDER BY type ASC, lower(name) ASC, created_at ASC;

-- name: UpdateCerebroCredentialMetadata :one
UPDATE cerebro_credential
SET name        = $2,
    description = $3,
    metadata    = $4,
    expires_at  = $5,
    updated_at  = now()
WHERE id = $1
RETURNING *;

-- name: RotateCerebroCredentialValue :one
UPDATE cerebro_credential
SET encrypted_value = $2,
    value_hint      = $3,
    last_rotated_at = now(),
    updated_at      = now()
WHERE id = $1
RETURNING *;

-- name: DeleteCerebroCredential :exec
DELETE FROM cerebro_credential WHERE id = $1;

-- name: CreateCerebroCredentialBinding :one
INSERT INTO cerebro_credential_binding (
    credential_id, resource_type, resource_id, created_by_type, created_by_id
)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (credential_id, resource_type, resource_id) DO UPDATE
SET created_by_type = EXCLUDED.created_by_type,
    created_by_id   = EXCLUDED.created_by_id
RETURNING *;

-- name: ListCerebroCredentialBindings :many
SELECT * FROM cerebro_credential_binding
WHERE credential_id = $1
ORDER BY created_at ASC, id ASC;

-- name: ListCerebroCredentialsForResource :many
SELECT c.*
FROM cerebro_credential c
JOIN cerebro_credential_binding b ON b.credential_id = c.id
WHERE b.resource_type = $1 AND b.resource_id = $2
ORDER BY c.type ASC, lower(c.name) ASC;

-- name: GetCerebroCredentialBinding :one
SELECT * FROM cerebro_credential_binding WHERE id = $1;

-- name: DeleteCerebroCredentialBinding :exec
DELETE FROM cerebro_credential_binding WHERE id = $1;

-- name: RecordCerebroCredentialAudit :one
INSERT INTO cerebro_credential_audit (
    workspace_id, credential_id, action, actor_type, actor_id, metadata
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListCerebroCredentialAudit :many
SELECT * FROM cerebro_credential_audit
WHERE credential_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2;

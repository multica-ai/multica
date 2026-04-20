-- name: GetExternalIdentityByProvider :one
SELECT * FROM external_identity
WHERE provider = $1
  AND provider_user_id = $2;

-- name: CreateExternalIdentity :one
INSERT INTO external_identity (
    user_id,
    provider,
    provider_user_id,
    union_id,
    tenant_key,
    email,
    name,
    avatar_url,
    raw_profile
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
RETURNING *;

-- name: UpdateExternalIdentity :one
UPDATE external_identity
SET
    union_id = COALESCE($2, union_id),
    tenant_key = COALESCE($3, tenant_key),
    email = COALESCE($4, email),
    name = COALESCE($5, name),
    avatar_url = COALESCE($6, avatar_url),
    raw_profile = COALESCE($7, raw_profile),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListExternalIdentitiesByUser :many
SELECT * FROM external_identity
WHERE user_id = $1
ORDER BY created_at ASC;

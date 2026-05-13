-- name: GetUserByExternalIdentity :one
SELECT u.*
FROM user_external_identity x
JOIN "user" u ON u.id = x.user_id
WHERE x.provider = $1
  AND x.tenant_key = $2
  AND (
    (sqlc.arg('union_id')::text <> '' AND x.union_id = sqlc.arg('union_id')::text)
    OR (sqlc.arg('open_id')::text <> '' AND x.open_id = sqlc.arg('open_id')::text)
  )
ORDER BY CASE
  WHEN sqlc.arg('union_id')::text <> '' AND x.union_id = sqlc.arg('union_id')::text THEN 0
  ELSE 1
END
LIMIT 1;

-- name: UpsertUserExternalIdentityByOpenID :one
INSERT INTO user_external_identity (
    user_id,
    provider,
    tenant_key,
    external_user_id,
    open_id,
    union_id,
    email,
    name,
    avatar_url,
    raw_profile,
    last_synced_at
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6,
    $7,
    $8,
    $9,
    $10,
    now()
)
ON CONFLICT (provider, tenant_key, open_id) DO UPDATE SET
    user_id = EXCLUDED.user_id,
    external_user_id = EXCLUDED.external_user_id,
    union_id = EXCLUDED.union_id,
    email = EXCLUDED.email,
    name = EXCLUDED.name,
    avatar_url = EXCLUDED.avatar_url,
    raw_profile = EXCLUDED.raw_profile,
    last_synced_at = now(),
    updated_at = now()
RETURNING *;

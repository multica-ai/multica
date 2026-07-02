-- name: GetConnectionPersonKey :one
SELECT id, workspace_id, connection_id, member_id, key_ciphertext, expires_at, created_at, updated_at
FROM cerebro_connection_person_key
WHERE connection_id = $1 AND member_id = $2;

-- name: UpsertConnectionPersonKey :one
INSERT INTO cerebro_connection_person_key (
    workspace_id,
    connection_id,
    member_id,
    key_ciphertext,
    expires_at
) VALUES (
    $1, $2, $3, $4, $5
)
ON CONFLICT (connection_id, member_id) DO UPDATE SET
    key_ciphertext = EXCLUDED.key_ciphertext,
    expires_at = EXCLUDED.expires_at,
    updated_at = now()
RETURNING id, workspace_id, connection_id, member_id, key_ciphertext, expires_at, created_at, updated_at;

-- name: DeleteConnectionPersonKeys :exec
DELETE FROM cerebro_connection_person_key
WHERE connection_id = $1;

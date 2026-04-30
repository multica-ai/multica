-- name: GetFixedLoginCodeByUserID :one
SELECT * FROM fixed_login_code
WHERE user_id = $1;

-- name: UpsertFixedLoginCode :one
INSERT INTO fixed_login_code (user_id, code_hash)
VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE
SET code_hash = EXCLUDED.code_hash,
    updated_at = now()
RETURNING *;

-- name: DeleteFixedLoginCode :exec
DELETE FROM fixed_login_code
WHERE user_id = $1;


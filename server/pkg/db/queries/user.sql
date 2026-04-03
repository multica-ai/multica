-- name: GetUser :one
SELECT * FROM user
WHERE id = ?;

-- name: GetUserByEmail :one
SELECT * FROM user
WHERE email = ?;

-- name: CreateUser :one
INSERT INTO user (id, name, email, avatar_url)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: UpdateUser :one
UPDATE user SET
    name = COALESCE(?, name),
    avatar_url = COALESCE(?, avatar_url),
    updated_at = datetime('now')
WHERE id = ?
RETURNING *;

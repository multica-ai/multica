-- name: CreateRuntimePermission :one
INSERT INTO multica_runtime_permission (runtime_id, user_id, role)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetRuntimePermission :one
SELECT * FROM multica_runtime_permission
WHERE runtime_id = $1 AND user_id = $2;

-- name: ListRuntimePermissions :many
SELECT rp.*, u.name AS user_name, u.email AS user_email
FROM multica_runtime_permission rp
JOIN multica_user u ON u.id = rp.user_id
WHERE rp.runtime_id = $1
ORDER BY rp.created_at ASC;

-- name: UpdateRuntimePermissionRole :one
UPDATE multica_runtime_permission
SET role = $3, updated_at = now()
WHERE runtime_id = $1 AND user_id = $2
RETURNING *;

-- name: DeleteRuntimePermission :exec
DELETE FROM multica_runtime_permission
WHERE runtime_id = $1 AND user_id = $2;

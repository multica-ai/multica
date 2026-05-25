-- name: ListCerebroRoles :many
SELECT id, workspace_id, name, description, created_by, created_at, updated_at
FROM cerebro_role
WHERE workspace_id = $1
ORDER BY lower(name), created_at ASC;

-- name: GetCerebroRole :one
SELECT id, workspace_id, name, description, created_by, created_at, updated_at
FROM cerebro_role
WHERE id = $1;

-- name: CreateCerebroRole :one
INSERT INTO cerebro_role (workspace_id, name, description, created_by)
VALUES ($1, $2, $3, $4)
RETURNING id, workspace_id, name, description, created_by, created_at, updated_at;

-- name: UpdateCerebroRole :one
UPDATE cerebro_role
SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING id, workspace_id, name, description, created_by, created_at, updated_at;

-- name: DeleteCerebroRole :exec
DELETE FROM cerebro_role
WHERE id = $1;

-- name: AssignCerebroRole :one
WITH inserted AS (
    INSERT INTO cerebro_role_assignment (role_id, subject_type, subject_id, added_by)
    VALUES ($1, $2, $3, $4)
    ON CONFLICT (role_id, subject_type, subject_id) DO NOTHING
    RETURNING role_id, subject_type, subject_id, added_by, added_at
)
SELECT role_id, subject_type, subject_id, added_by, added_at FROM inserted
UNION ALL
SELECT role_id, subject_type, subject_id, added_by, added_at
FROM cerebro_role_assignment
WHERE role_id = $1 AND subject_type = $2 AND subject_id = $3
  AND NOT EXISTS (SELECT 1 FROM inserted)
LIMIT 1;

-- name: UnassignCerebroRole :one
DELETE FROM cerebro_role_assignment
WHERE role_id = $1 AND subject_type = $2 AND subject_id = $3
RETURNING role_id, subject_type, subject_id, added_by, added_at;

-- name: ListCerebroRoleAssignments :many
SELECT role_id, subject_type, subject_id, added_by, added_at
FROM cerebro_role_assignment
WHERE role_id = $1
ORDER BY subject_type, added_at ASC;

-- name: ListCerebroRoleIDsForSubject :many
SELECT ra.role_id
FROM cerebro_role_assignment ra
JOIN cerebro_role r ON r.id = ra.role_id
WHERE r.workspace_id = $1
  AND ra.subject_type = $2
  AND ra.subject_id = $3
ORDER BY ra.role_id;

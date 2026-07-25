-- name: ListCerebroRoles :many
SELECT id, workspace_id, name, description, created_by, created_at, updated_at,
       version, permissions, archived_at
FROM cerebro_role
WHERE workspace_id = sqlc.arg('workspace_id')
  AND (sqlc.arg('include_archived')::boolean OR archived_at IS NULL)
ORDER BY lower(name), created_at ASC;

-- name: GetCerebroRole :one
SELECT id, workspace_id, name, description, created_by, created_at, updated_at,
       version, permissions, archived_at
FROM cerebro_role
WHERE id = $1;

-- name: CreateCerebroRole :one
INSERT INTO cerebro_role (workspace_id, name, description, created_by, permissions)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, workspace_id, name, description, created_by, created_at, updated_at,
          version, permissions, archived_at;

-- name: UpdateCerebroRole :one
UPDATE cerebro_role
SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    permissions = CASE
        WHEN sqlc.arg('replace_permissions')::boolean
            THEN sqlc.arg('permissions')::jsonb
        ELSE permissions
    END,
    version = version + 1,
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND archived_at IS NULL
RETURNING id, workspace_id, name, description, created_by, created_at, updated_at,
          version, permissions, archived_at;

-- name: ArchiveCerebroRole :one
UPDATE cerebro_role
SET archived_at = COALESCE(archived_at, now()),
    version = CASE WHEN archived_at IS NULL THEN version + 1 ELSE version END,
    updated_at = now()
WHERE id = $1
RETURNING id, workspace_id, name, description, created_by, created_at, updated_at,
          version, permissions, archived_at;

-- name: AssignCerebroRole :one
WITH inserted AS (
    INSERT INTO cerebro_role_assignment (role_id, subject_type, subject_id, added_by, expires_at)
    VALUES ($1, $2, $3, $4, $5)
    ON CONFLICT (role_id, subject_type, subject_id) DO UPDATE SET
        added_by = EXCLUDED.added_by,
        added_at = now(),
        expires_at = EXCLUDED.expires_at
    RETURNING role_id, subject_type, subject_id, added_by, added_at, expires_at
)
SELECT role_id, subject_type, subject_id, added_by, added_at, expires_at
FROM inserted;

-- name: UnassignCerebroRole :one
DELETE FROM cerebro_role_assignment
WHERE role_id = $1 AND subject_type = $2 AND subject_id = $3
RETURNING role_id, subject_type, subject_id, added_by, added_at, expires_at;

-- name: ListCerebroRoleAssignments :many
SELECT role_id, subject_type, subject_id, added_by, added_at, expires_at
FROM cerebro_role_assignment
WHERE role_id = $1
ORDER BY subject_type, added_at ASC;

-- name: ListCerebroRoleAssignmentsWithNames :many
-- Display variant: resolves the assigned subject's human-readable name (member
-- -> user, agent) so the Roles tab never shows a raw UUID. Role assignments
-- only ever target members and agents.
SELECT
    ra.role_id, ra.subject_type, ra.subject_id, ra.added_by, ra.added_at,
    ra.expires_at,
    COALESCE(
        NULLIF(u.name, ''),
        NULLIF(ag.name, ''),
        ''
    )::text AS subject_display_name
FROM cerebro_role_assignment ra
LEFT JOIN "user" u
  ON ra.subject_type = 'member' AND u.id = ra.subject_id
LEFT JOIN agent ag
  ON ra.subject_type = 'agent' AND ag.id = ra.subject_id
WHERE ra.role_id = $1
ORDER BY ra.subject_type, ra.added_at ASC;

-- name: ListCerebroRoleIDsForSubject :many
SELECT ra.role_id
FROM cerebro_role_assignment ra
JOIN cerebro_role r ON r.id = ra.role_id
WHERE r.workspace_id = $1
  AND r.archived_at IS NULL
  AND (ra.expires_at IS NULL OR ra.expires_at > now())
  AND ra.subject_type = $2
  AND ra.subject_id = $3
ORDER BY ra.role_id;

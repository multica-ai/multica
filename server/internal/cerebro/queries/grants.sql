-- Workspace grant CRUD (JEH-1179).

-- name: CreateCerebroWorkspaceGrant :one
INSERT INTO cerebro_workspace_grant (
    workspace_id,
    subject_type, subject_id,
    resource_pattern, capability,
    classification_ceiling,
    time_window_start, time_window_end,
    approval_required,
    granted_by_type, granted_by_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
)
RETURNING
    id, workspace_id,
    subject_type, subject_id,
    resource_pattern, capability,
    classification_ceiling,
    time_window_start, time_window_end,
    approval_required,
    status,
    granted_by_type, granted_by_id, granted_at,
    revoked_by_id, revoked_at,
    updated_at;

-- name: GetCerebroWorkspaceGrant :one
SELECT
    id, workspace_id,
    subject_type, subject_id,
    resource_pattern, capability,
    classification_ceiling,
    time_window_start, time_window_end,
    approval_required,
    status,
    granted_by_type, granted_by_id, granted_at,
    revoked_by_id, revoked_at,
    updated_at
FROM cerebro_workspace_grant
WHERE id = $1 AND workspace_id = $2;

-- name: ListCerebroWorkspaceGrants :many
SELECT
    id, workspace_id,
    subject_type, subject_id,
    resource_pattern, capability,
    classification_ceiling,
    time_window_start, time_window_end,
    approval_required,
    status,
    granted_by_type, granted_by_id, granted_at,
    revoked_by_id, revoked_at,
    updated_at
FROM cerebro_workspace_grant
WHERE workspace_id = $1
  -- CEREBRO-PATCH(persona-grants-list-filters): treat omitted query params as unfiltered.
  AND (NULLIF($2::text, '') IS NULL OR subject_type = $2)
  AND ($3::uuid  IS NULL OR subject_id   = $3)
  AND (NULLIF($4::text, '') IS NULL OR status       = $4)
ORDER BY granted_at DESC;

-- name: UpdateCerebroWorkspaceGrant :one
UPDATE cerebro_workspace_grant
SET
    resource_pattern       = COALESCE($3, resource_pattern),
    capability             = COALESCE($4, capability),
    classification_ceiling = COALESCE($5, classification_ceiling),
    time_window_start      = COALESCE($6, time_window_start),
    time_window_end        = COALESCE($7, time_window_end),
    approval_required      = COALESCE($8, approval_required),
    updated_at             = now()
WHERE id = $1 AND workspace_id = $2
RETURNING
    id, workspace_id,
    subject_type, subject_id,
    resource_pattern, capability,
    classification_ceiling,
    time_window_start, time_window_end,
    approval_required,
    status,
    granted_by_type, granted_by_id, granted_at,
    revoked_by_id, revoked_at,
    updated_at;

-- name: RevokeCerebroWorkspaceGrant :one
UPDATE cerebro_workspace_grant
SET
    status       = 'revoked',
    revoked_by_id = $3,
    revoked_at   = now(),
    updated_at   = now()
WHERE id = $1 AND workspace_id = $2
RETURNING
    id, workspace_id,
    subject_type, subject_id,
    resource_pattern, capability,
    classification_ceiling,
    time_window_start, time_window_end,
    approval_required,
    status,
    granted_by_type, granted_by_id, granted_at,
    revoked_by_id, revoked_at,
    updated_at;

-- name: DeleteCerebroWorkspaceGrant :exec
DELETE FROM cerebro_workspace_grant
WHERE id = $1 AND workspace_id = $2;

-- Audit

-- name: ListCerebroGrantAudit :many
SELECT
    a.id, a.workspace_id, a.grant_id, a.action,
    a.actor_type, a.actor_id,
    COALESCE(u.name, ag.name, '') AS actor_name,
    a.surface, a.diff, a.created_at,
    count(*) OVER()::int AS total
FROM cerebro_workspace_grant_audit a
LEFT JOIN cerebro_workspace_grant g
  ON g.id = a.grant_id
LEFT JOIN "user" u
  ON a.actor_type = 'member' AND u.id = a.actor_id
LEFT JOIN agent ag
  ON a.actor_type = 'agent' AND ag.id = a.actor_id
WHERE a.workspace_id = $1
  AND ($2::uuid IS NULL OR g.subject_id = $2)
  AND ($3::uuid IS NULL OR a.grant_id = $3)
  AND ($4::timestamptz IS NULL OR a.created_at >= $4)
ORDER BY a.created_at DESC
LIMIT $5 OFFSET $6;

-- name: InsertCerebroGrantAudit :exec
INSERT INTO cerebro_workspace_grant_audit (
    workspace_id, grant_id, action,
    actor_type, actor_id,
    surface, diff
) VALUES ($1, $2, $3, $4, $5, $6, $7);

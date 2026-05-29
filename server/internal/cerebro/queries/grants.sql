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

-- name: GetCerebroWorkspaceGrantWithName :one
-- Display variant of GetCerebroWorkspaceGrant for the Access grant drawer.
-- Resolves the subject's human-readable name so the drawer never shows a raw
-- UUID. GetCerebroWorkspaceGrant is left untouched: the runtime gateway depends
-- on its exact row shape.
SELECT
    g.id, g.workspace_id,
    g.subject_type, g.subject_id,
    g.resource_pattern, g.capability,
    g.classification_ceiling,
    g.time_window_start, g.time_window_end,
    g.approval_required,
    g.status,
    g.granted_by_type, g.granted_by_id, g.granted_at,
    g.revoked_by_id, g.revoked_at,
    g.updated_at,
    COALESCE(
        NULLIF(u.name, ''),
        NULLIF(ag.name, ''),
        NULLIF(grp.name, ''),
        NULLIF(rl.name, ''),
        ''
    )::text AS subject_display_name
FROM cerebro_workspace_grant g
LEFT JOIN "user" u
  ON g.subject_type = 'member' AND u.id = g.subject_id
LEFT JOIN agent ag
  ON g.subject_type = 'agent' AND ag.id = g.subject_id
LEFT JOIN cerebro_group grp
  ON g.subject_type = 'group' AND grp.id = g.subject_id
LEFT JOIN cerebro_role rl
  ON g.subject_type = 'role' AND rl.id = g.subject_id
WHERE g.id = $1 AND g.workspace_id = $2;

-- name: ListCerebroWorkspaceGrantsWithNames :many
-- Display variant of ListCerebroWorkspaceGrants for the Access UI. Resolves the
-- subject's human-readable name (member -> user, agent, group, role) so the
-- table never shows a raw UUID. The plain ListCerebroWorkspaceGrants is left
-- untouched on purpose: the permission resolver depends on its exact row shape.
SELECT
    g.id, g.workspace_id,
    g.subject_type, g.subject_id,
    g.resource_pattern, g.capability,
    g.classification_ceiling,
    g.time_window_start, g.time_window_end,
    g.approval_required,
    g.status,
    g.granted_by_type, g.granted_by_id, g.granted_at,
    g.revoked_by_id, g.revoked_at,
    g.updated_at,
    COALESCE(
        NULLIF(u.name, ''),
        NULLIF(ag.name, ''),
        NULLIF(grp.name, ''),
        NULLIF(rl.name, ''),
        ''
    )::text AS subject_display_name
FROM cerebro_workspace_grant g
LEFT JOIN "user" u
  ON g.subject_type = 'member' AND u.id = g.subject_id
LEFT JOIN agent ag
  ON g.subject_type = 'agent' AND ag.id = g.subject_id
LEFT JOIN cerebro_group grp
  ON g.subject_type = 'group' AND grp.id = g.subject_id
LEFT JOIN cerebro_role rl
  ON g.subject_type = 'role' AND rl.id = g.subject_id
WHERE g.workspace_id = $1
  AND (NULLIF($2::text, '') IS NULL OR g.subject_type = $2)
  AND ($3::uuid  IS NULL OR g.subject_id   = $3)
  AND (NULLIF($4::text, '') IS NULL OR g.status       = $4)
ORDER BY g.granted_at DESC;

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
  AND ($5::timestamptz IS NULL OR a.created_at <= $5)
  AND (NULLIF($6::text, '') IS NULL OR g.capability = $6)
ORDER BY a.created_at DESC
LIMIT $7 OFFSET $8;

-- name: InsertCerebroGrantAudit :exec
INSERT INTO cerebro_workspace_grant_audit (
    workspace_id, grant_id, action,
    actor_type, actor_id,
    surface, diff
) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: InsertCerebroPermissionAuditEvent :exec
INSERT INTO activity_log (
    workspace_id,
    actor_type,
    actor_id,
    action,
    details
) VALUES (
    $1,
    $2,
    $3,
    'permission_policy_evaluated',
    $4
);

-- Collections, Phase 2 — per-project access grants (FIR-2125).
-- Mirrors folder_grant.sql but uses the project_nesting tree for inheritance.
-- NOTE: These queries are used as raw pgx queries in the projectgrant handler
-- (not yet sqlc-generated). Run `make sqlc` if you want generated code.

-- name: ListCerebroProjectDirectGrants :many
-- Grants set directly on this project — the "This project" tab.
SELECT id, workspace_id, project_id, grantee_type, grantee_id, role, created_at, created_by
FROM cerebro_project_grant
WHERE workspace_id = $1 AND project_id = $2
ORDER BY grantee_type, created_at;

-- name: ListCerebroProjectEffectiveGrants :many
-- Direct + inherited grants for a project. Walks the project's ancestors via
-- project_nesting; is_direct = false means the grant comes from a parent project.
-- depth 0 = the project itself, so the caller can pick the nearest grant per grantee.
WITH RECURSIVE ancestors AS (
    SELECT project_id AS id, 0 AS depth
    FROM project_nesting
    WHERE project_id = $1
    UNION ALL
    SELECT pn.parent_project_id, a.depth + 1
    FROM project_nesting pn
    JOIN ancestors a ON pn.project_id = a.id
    WHERE pn.parent_project_id IS NOT NULL AND a.depth < 3
)
SELECT g.grantee_type, g.grantee_id, g.role,
       g.project_id AS source_project_id,
       (a.depth = 0) AS is_direct,
       a.depth AS depth
FROM ancestors a
JOIN cerebro_project_grant g ON g.project_id = a.id
  AND g.workspace_id = $2
ORDER BY a.depth ASC, g.grantee_type;

-- name: UpsertCerebroProjectGrant :one
INSERT INTO cerebro_project_grant (workspace_id, project_id, grantee_type, grantee_id, role, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (project_id, grantee_type,
             COALESCE(grantee_id, '00000000-0000-0000-0000-000000000000'::uuid))
DO UPDATE SET role = EXCLUDED.role
RETURNING *;

-- name: RemoveCerebroProjectGrant :exec
DELETE FROM cerebro_project_grant
WHERE workspace_id = $1 AND project_id = $2 AND grantee_type = $3
  AND COALESCE(grantee_id, '00000000-0000-0000-0000-000000000000'::uuid)
    = COALESCE($4, '00000000-0000-0000-0000-000000000000'::uuid);

-- name: DeleteAllCerebroProjectGrants :exec
DELETE FROM cerebro_project_grant
WHERE workspace_id = $1 AND project_id = $2;

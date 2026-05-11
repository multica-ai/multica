-- CEREBRO-PATCH(nested-projects): fork-specific nested projects companion queries.
-- Kept in a dedicated file so upstream-merges don't conflict on the canonical project.sql.

-- name: GetProjectNesting :one
SELECT * FROM project_nesting
WHERE project_id = $1;

-- name: UpsertProjectNesting :one
INSERT INTO project_nesting (project_id, parent_project_id, show_descendants, depth)
VALUES ($1, $2, $3, $4)
ON CONFLICT (project_id) DO UPDATE
  SET parent_project_id = EXCLUDED.parent_project_id,
      show_descendants  = EXCLUDED.show_descendants,
      depth             = EXCLUDED.depth,
      updated_at        = now()
RETURNING *;

-- name: UpdateProjectNestingDepth :exec
UPDATE project_nesting
SET depth = $2,
    updated_at = now()
WHERE project_id = $1;

-- name: ListProjectNesting :many
SELECT pn.*
FROM project_nesting pn
JOIN project p ON p.id = pn.project_id
WHERE p.workspace_id = $1;

-- name: GetProjectRollupStats :one
WITH RECURSIVE descendants AS (
    SELECT $1::uuid AS id
    UNION ALL
    SELECT pn.project_id
      FROM project_nesting pn
      JOIN descendants d ON pn.parent_project_id = d.id
)
SELECT
    COUNT(*)::bigint AS total_count,
    COUNT(*) FILTER (WHERE i.status IN ('done', 'cancelled'))::bigint AS done_count
FROM issue i
WHERE i.kind = 'issue'
  AND i.project_id IN (SELECT id FROM descendants);

-- name: ListProjectAndDescendantIDs :many
WITH RECURSIVE descendants AS (
    SELECT $1::uuid AS id
    UNION ALL
    SELECT pn.project_id
      FROM project_nesting pn
      JOIN descendants d ON pn.parent_project_id = d.id
)
SELECT id FROM descendants;

-- name: ListViews :many
-- A view is visible to a member when it is shared (workspace-wide) or they own
-- it. Private views (shared = false) never leak to other members.
SELECT * FROM saved_view
WHERE workspace_id = $1
  AND page = $2
  AND project_id IS NOT DISTINCT FROM sqlc.narg('project_id')::uuid
  AND (shared OR creator_id = sqlc.arg('viewer_id')::uuid)
ORDER BY position ASC, created_at ASC;

-- name: GetView :one
SELECT * FROM saved_view
WHERE id = $1 AND workspace_id = $2;

-- name: CreateView :one
INSERT INTO saved_view (
    workspace_id, creator_id, name, page, project_id, filters, display, position, shared
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: UpdateView :one
UPDATE saved_view SET
    name = COALESCE(sqlc.narg('name'), name),
    filters = COALESCE(sqlc.narg('filters')::jsonb, filters),
    display = COALESCE(sqlc.narg('display')::jsonb, display),
    position = COALESCE(sqlc.narg('position'), position),
    shared = COALESCE(sqlc.narg('shared'), shared),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: ReorderViews :exec
-- Single atomic statement: assign each view its 1-based ordinal from the id
-- array. A bulk UPDATE is transactional by definition, so a partial reorder
-- can never be observed. Ids not in this workspace are ignored by the join.
UPDATE saved_view AS sv
SET position = v.ord, updated_at = now()
FROM unnest(@ids::uuid[]) WITH ORDINALITY AS v(id, ord)
WHERE sv.id = v.id AND sv.workspace_id = @workspace_id;

-- name: DeleteView :one
DELETE FROM saved_view
WHERE id = $1 AND workspace_id = $2
RETURNING id;

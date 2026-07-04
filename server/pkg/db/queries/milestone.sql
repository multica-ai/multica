-- name: ListMilestonesByProject :many
SELECT * FROM milestone
WHERE project_id = $1
ORDER BY sort_order ASC, created_at DESC;

-- name: GetMilestone :one
SELECT * FROM milestone
WHERE id = $1;

-- name: CreateMilestone :one
INSERT INTO milestone (
  project_id, title, description, start_date, due_date, status, sort_order, created_by
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8
)
RETURNING *;

-- name: UpdateMilestone :one
UPDATE milestone
SET title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    start_date = COALESCE(sqlc.narg('start_date'), start_date),
    due_date = COALESCE(sqlc.narg('due_date'), due_date),
    status = COALESCE(sqlc.narg('status'), status),
    sort_order = COALESCE(sqlc.narg('sort_order'), sort_order),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteMilestone :exec
DELETE FROM milestone
WHERE id = $1;

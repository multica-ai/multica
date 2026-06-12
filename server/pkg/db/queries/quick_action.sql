-- name: ListQuickActions :many
SELECT * FROM quick_action
WHERE workspace_id = $1
ORDER BY created_at ASC, id ASC;

-- name: CreateQuickAction :one
INSERT INTO quick_action (workspace_id, label, body)
VALUES ($1, $2, $3)
RETURNING *;

-- name: UpdateQuickAction :one
UPDATE quick_action SET
    label = COALESCE(sqlc.narg('label'), label),
    body = COALESCE(sqlc.narg('body'), body),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteQuickAction :one
-- :one RETURNING id so the handler distinguishes pgx.ErrNoRows (→ 404) from
-- infrastructure errors (→ 500), and avoids a TOCTOU precheck.
DELETE FROM quick_action
WHERE id = $1 AND workspace_id = $2
RETURNING id;

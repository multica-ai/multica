-- name: ListActivities :many
SELECT * FROM activity_log
WHERE issue_id = ?
ORDER BY created_at ASC
LIMIT ? OFFSET ?;

-- name: CreateActivity :one
INSERT INTO activity_log (
    id, workspace_id, issue_id, actor_type, actor_id, action, details
) VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

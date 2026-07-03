-- name: CreateTaskMessage :one
INSERT INTO task_message (task_id, seq, type, tool, content, input, output, call_id, is_error)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListTaskMessages :many
SELECT * FROM task_message
WHERE task_id = $1
ORDER BY seq ASC;

-- name: ListTaskMessagesSince :many
SELECT * FROM task_message
WHERE task_id = $1 AND seq > $2
ORDER BY seq ASC;

-- name: DeleteTaskMessages :exec
DELETE FROM task_message
WHERE task_id = $1;

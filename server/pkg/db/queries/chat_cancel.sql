-- name: HasTaskMessages :one
-- Cancellation only needs to know whether a transcript exists.
SELECT EXISTS (SELECT 1 FROM task_message WHERE task_id = $1);

-- name: UpsertTaskContextReceipt :exec
-- One receipt per task (MUL-9013). A re-report for the same task — the
-- fresh-session retry after a rejected resume — replaces the first.
INSERT INTO task_context_receipt (task_id, workspace_id, issue_id, receipt)
VALUES (@task_id, @workspace_id, sqlc.narg(issue_id), @receipt)
ON CONFLICT (task_id) DO UPDATE
SET receipt = EXCLUDED.receipt,
    updated_at = now();

-- name: GetTaskContextReceipt :one
SELECT task_id, workspace_id, receipt, created_at, updated_at
FROM task_context_receipt
WHERE task_id = $1;

-- name: GetAutomationRule :one
SELECT * FROM automation_rule
WHERE workspace_id = $1 AND template_id = $2
LIMIT 1;

-- name: ListAutomationRules :many
SELECT * FROM automation_rule
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: UpsertAutomationRule :one
INSERT INTO automation_rule (workspace_id, template_id, enabled, created_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (workspace_id, template_id)
DO UPDATE SET
    enabled = EXCLUDED.enabled,
    updated_at = now()
RETURNING *;

-- name: DeleteAutomationRule :exec
DELETE FROM automation_rule
WHERE workspace_id = $1 AND template_id = $2;

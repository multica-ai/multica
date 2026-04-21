-- name: ListColumnConfigs :many
SELECT * FROM workspace_column_config
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: UpsertColumnConfig :one
INSERT INTO workspace_column_config (
    workspace_id,
    status,
    instructions,
    allowed_transitions
) VALUES ($1, $2, $3, sqlc.arg(allowed_transitions)::text[])
ON CONFLICT ON CONSTRAINT workspace_column_config_unique
DO UPDATE SET
    instructions = EXCLUDED.instructions,
    allowed_transitions = EXCLUDED.allowed_transitions,
    updated_at = NOW()
RETURNING *;

-- name: GetColumnConfig :one
SELECT * FROM workspace_column_config
WHERE workspace_id = $1 AND status = $2;

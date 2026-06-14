-- name: GetEidetixConfigForProject :one
SELECT * FROM eidetix_project_config WHERE project_id = $1;

-- name: UpsertEidetixProjectConfig :one
INSERT INTO eidetix_project_config (
    project_id, enabled, endpoint_url, token_encrypted, graph_label
) VALUES (
    $1, sqlc.arg('enabled'), sqlc.arg('endpoint_url'), sqlc.arg('token_encrypted'), sqlc.narg('graph_label')
)
ON CONFLICT (project_id) DO UPDATE SET
    enabled         = EXCLUDED.enabled,
    endpoint_url    = EXCLUDED.endpoint_url,
    token_encrypted = EXCLUDED.token_encrypted,
    graph_label     = EXCLUDED.graph_label,
    updated_at      = now()
RETURNING *;

-- name: SetEidetixProjectEnabled :one
UPDATE eidetix_project_config
SET enabled = $2, updated_at = now()
WHERE project_id = $1
RETURNING *;

-- name: DeleteEidetixProjectConfig :exec
DELETE FROM eidetix_project_config WHERE project_id = $1;

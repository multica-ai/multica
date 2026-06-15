-- name: GetEidetixConfigForProject :one
SELECT * FROM eidetix_project_config WHERE project_id = $1;

-- name: UpsertEidetixProjectConfig :one
-- endpoint_url and graph_label are nullable inputs: on a re-set (e.g. rotating
-- the token) where they are not supplied, the existing values are preserved
-- rather than reset. This matches the table's soft-config philosophy — you can
-- rotate the token without losing the endpoint/label.
INSERT INTO eidetix_project_config (
    project_id, enabled, endpoint_url, token_encrypted, graph_label
) VALUES (
    $1, sqlc.arg('enabled'),
    COALESCE(sqlc.narg('endpoint_url')::text, 'https://eidetix.nodeops.xyz/mcp/sse'),
    sqlc.arg('token_encrypted'), sqlc.narg('graph_label')
)
ON CONFLICT (project_id) DO UPDATE SET
    enabled         = EXCLUDED.enabled,
    endpoint_url    = COALESCE(sqlc.narg('endpoint_url')::text, eidetix_project_config.endpoint_url),
    token_encrypted = EXCLUDED.token_encrypted,
    graph_label     = COALESCE(sqlc.narg('graph_label'), eidetix_project_config.graph_label),
    updated_at      = now()
RETURNING *;

-- name: SetEidetixProjectEnabled :one
UPDATE eidetix_project_config
SET enabled = $2, updated_at = now()
WHERE project_id = $1
RETURNING *;

-- name: DeleteEidetixProjectConfig :exec
DELETE FROM eidetix_project_config WHERE project_id = $1;

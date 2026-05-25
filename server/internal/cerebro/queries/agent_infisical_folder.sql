-- name: ListAgentInfisicalFolders :many
SELECT id, agent_id, environment, secret_path, created_at, updated_at
FROM cerebro_agent_infisical_folder
WHERE agent_id = $1
ORDER BY environment, secret_path;

-- name: DeleteAgentInfisicalFolders :exec
DELETE FROM cerebro_agent_infisical_folder
WHERE agent_id = $1;

-- name: InsertAgentInfisicalFolder :one
INSERT INTO cerebro_agent_infisical_folder (
    agent_id,
    environment,
    secret_path
) VALUES (
    $1,
    $2,
    $3
)
ON CONFLICT (agent_id, environment, secret_path)
DO UPDATE SET updated_at = now()
RETURNING id, agent_id, environment, secret_path, created_at, updated_at;

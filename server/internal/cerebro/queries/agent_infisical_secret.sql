-- name: ListAgentInfisicalSecrets :many
SELECT id, agent_id, env_var_name, secret_name, environment, secret_path, created_at, updated_at
FROM cerebro_agent_infisical_secret
WHERE agent_id = $1
ORDER BY env_var_name;

-- name: DeleteAgentInfisicalSecrets :exec
DELETE FROM cerebro_agent_infisical_secret
WHERE agent_id = $1;

-- name: UpsertAgentInfisicalSecret :one
INSERT INTO cerebro_agent_infisical_secret (
    agent_id,
    env_var_name,
    secret_name,
    environment,
    secret_path
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5
)
ON CONFLICT (agent_id, upper(env_var_name))
DO UPDATE SET
    env_var_name = EXCLUDED.env_var_name,
    secret_name = EXCLUDED.secret_name,
    environment = EXCLUDED.environment,
    secret_path = EXCLUDED.secret_path,
    updated_at = now()
RETURNING id, agent_id, env_var_name, secret_name, environment, secret_path, created_at, updated_at;

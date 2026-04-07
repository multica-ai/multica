-- Local Agent Config CRUD

-- name: ListLocalAgentConfigs :many
SELECT * FROM local_agent_config
WHERE workspace_id = $1
ORDER BY provider ASC;

-- name: GetLocalAgentConfig :one
SELECT * FROM local_agent_config
WHERE workspace_id = $1 AND provider = $2;

-- name: UpsertLocalAgentConfig :one
INSERT INTO local_agent_config (workspace_id, provider, cli_path, version, status, is_custom_path, last_health_check, health_error)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (workspace_id, provider)
DO UPDATE SET
    cli_path = EXCLUDED.cli_path,
    version = EXCLUDED.version,
    status = EXCLUDED.status,
    is_custom_path = EXCLUDED.is_custom_path,
    last_health_check = EXCLUDED.last_health_check,
    health_error = EXCLUDED.health_error,
    updated_at = now()
RETURNING *;

-- name: UpdateLocalAgentConfigPath :one
UPDATE local_agent_config
SET cli_path = $3, is_custom_path = true, updated_at = now()
WHERE workspace_id = $1 AND provider = $2
RETURNING *;

-- name: UpdateLocalAgentHealthCheck :exec
UPDATE local_agent_config
SET status = $3, version = $4, last_health_check = now(), health_error = $5, updated_at = now()
WHERE workspace_id = $1 AND provider = $2;

-- Local Skill CRUD

-- name: ListLocalSkills :many
SELECT * FROM local_skill
WHERE (workspace_id = $1 OR workspace_id IS NULL)
  AND (project_path = sqlc.narg('project_path') OR project_path IS NULL)
ORDER BY is_default DESC, name ASC;

-- name: ListGlobalLocalSkills :many
SELECT * FROM local_skill
WHERE workspace_id IS NULL
ORDER BY name ASC;

-- name: GetLocalSkill :one
SELECT * FROM local_skill
WHERE id = $1;

-- name: CreateLocalSkill :one
INSERT INTO local_skill (workspace_id, project_path, name, description, content, is_default)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateLocalSkill :one
UPDATE local_skill SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    content = COALESCE(sqlc.narg('content'), content),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteLocalSkill :exec
DELETE FROM local_skill WHERE id = $1;

-- name: ListAgentConfigTemplates :many
SELECT * FROM agent_config_template
WHERE workspace_id = $1
  AND (sqlc.narg('scope')::varchar IS NULL OR scope = sqlc.narg('scope')::varchar)
ORDER BY created_at ASC;

-- name: GetAgentConfigTemplate :one
SELECT * FROM agent_config_template
WHERE id = $1;

-- name: GetAgentConfigTemplateInWorkspace :one
SELECT * FROM agent_config_template
WHERE id = $1 AND workspace_id = $2;

-- name: GetDefaultSystemTemplate :one
SELECT * FROM agent_config_template
WHERE workspace_id = $1 AND scope = 'system' AND is_default = true
LIMIT 1;

-- name: GetDefaultPersonalTemplate :one
SELECT * FROM agent_config_template
WHERE workspace_id = $1 AND scope = 'personal' AND is_default = true AND created_by = $2
LIMIT 1;

-- name: CreateAgentConfigTemplate :one
INSERT INTO agent_config_template (
    workspace_id, scope, name, description, config, is_default, created_by
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateAgentConfigTemplate :one
UPDATE agent_config_template SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    config = COALESCE(sqlc.narg('config'), config),
    is_default = COALESCE(sqlc.narg('is_default'), is_default),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteAgentConfigTemplate :exec
DELETE FROM agent_config_template
WHERE id = $1;

-- name: CountAgentTemplateReferences :one
-- Count how many agents reference this template (system or personal)
SELECT count(*) FROM agent
WHERE system_template_id = $1 OR personal_template_id = $1;

-- name: UpdateAgentTemplateBinding :one
UPDATE agent SET
    system_template_id = COALESCE(sqlc.narg('system_template_id'), system_template_id),
    personal_template_id = COALESCE(sqlc.narg('personal_template_id'), personal_template_id),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ClearAgentSystemTemplate :one
UPDATE agent SET system_template_id = NULL, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ClearAgentPersonalTemplate :one
UPDATE agent SET personal_template_id = NULL, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListAgentConfigTemplatesByCreator :many
SELECT * FROM agent_config_template
WHERE workspace_id = $1 AND created_by = $2
ORDER BY created_at ASC;

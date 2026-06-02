-- name: ListWorkflowPromptTemplatesByWorkspace :many
SELECT * FROM workflow_prompt_templates
WHERE workspace_id = @workspace_id
ORDER BY stage ASC, name ASC;

-- name: GetWorkflowPromptTemplate :one
SELECT * FROM workflow_prompt_templates
WHERE id = @id;

-- name: CreateWorkflowPromptTemplate :one
INSERT INTO workflow_prompt_templates (
    workspace_id, stage, name, content, is_default, version
) VALUES (
    @workspace_id, @stage, @name, @content,
    COALESCE(sqlc.narg('is_default')::boolean, false),
    COALESCE(sqlc.narg('version')::integer, 1)
)
RETURNING *;

-- name: UpdateWorkflowPromptTemplate :one
UPDATE workflow_prompt_templates
SET content = COALESCE(sqlc.narg('content'), content),
    name = COALESCE(sqlc.narg('name'), name),
    stage = COALESCE(sqlc.narg('stage'), stage),
    is_default = COALESCE(sqlc.narg('is_default')::boolean, is_default),
    version = version + 1,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteWorkflowPromptTemplate :exec
DELETE FROM workflow_prompt_templates
WHERE id = @id;

-- name: GetDefaultWorkflowPromptTemplateByStage :one
SELECT * FROM workflow_prompt_templates
WHERE workspace_id = @workspace_id
  AND stage = @stage
  AND is_default = true
LIMIT 1;

-- name: ListWorkflowPromptOverrides :many
SELECT * FROM workflow_prompt_overrides
WHERE template_id = @template_id
ORDER BY id ASC;

-- name: GetWorkflowPromptOverride :one
SELECT * FROM workflow_prompt_overrides
WHERE id = @id;

-- name: CreateWorkflowPromptOverride :one
INSERT INTO workflow_prompt_overrides (
    template_id, agent_id, project_id, override_content
) VALUES (
    @template_id,
    sqlc.narg('agent_id'),
    sqlc.narg('project_id'),
    @override_content
)
RETURNING *;

-- name: UpsertWorkflowPromptOverride :one
INSERT INTO workflow_prompt_overrides (
    template_id, agent_id, project_id, override_content
) VALUES (
    @template_id,
    sqlc.narg('agent_id'),
    sqlc.narg('project_id'),
    @override_content
)
ON CONFLICT (template_id,
        (COALESCE(agent_id, '00000000-0000-0000-0000-000000000000'::uuid)),
        (COALESCE(project_id, '00000000-0000-0000-0000-000000000000'::uuid)))
DO UPDATE SET override_content = EXCLUDED.override_content
RETURNING *;

-- name: DeleteWorkflowPromptOverride :exec
DELETE FROM workflow_prompt_overrides
WHERE id = @id;

-- name: ListAutopilotTemplates :many
SELECT * FROM autopilot_template
WHERE workspace_id = $1
ORDER BY name ASC, created_at ASC;

-- name: GetAutopilotTemplateInWorkspace :one
SELECT * FROM autopilot_template
WHERE id = $1 AND workspace_id = $2;

-- name: CreateAutopilotTemplate :one
INSERT INTO autopilot_template (
  workspace_id, name, description, execution_mode, issue_title_template,
  trigger_kind, cron_expression, timezone, created_by
) VALUES (
  $1, $2, $3, $4, sqlc.narg('issue_title_template'), $5,
  sqlc.narg('cron_expression'), sqlc.narg('timezone'), $6
)
RETURNING *;

-- name: UpdateAutopilotTemplate :one
UPDATE autopilot_template SET
  name = COALESCE(sqlc.narg('name'), name),
  description = COALESCE(sqlc.narg('description'), description),
  execution_mode = COALESCE(sqlc.narg('execution_mode'), execution_mode),
  -- PUT is a full replacement: NULL intentionally clears a prior title template.
  issue_title_template = sqlc.narg('issue_title_template'),
  trigger_kind = COALESCE(sqlc.narg('trigger_kind'), trigger_kind),
  cron_expression = CASE
    WHEN sqlc.narg('trigger_kind')::text = 'webhook' THEN NULL
    ELSE COALESCE(sqlc.narg('cron_expression'), cron_expression)
  END,
  timezone = CASE
    WHEN sqlc.narg('trigger_kind')::text = 'webhook' THEN NULL
    ELSE COALESCE(sqlc.narg('timezone'), timezone)
  END,
  updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteAutopilotTemplate :execrows
DELETE FROM autopilot_template
WHERE id = $1 AND workspace_id = $2;

-- name: ListAutopilots :many
SELECT * FROM autopilot
WHERE workspace_id = $1
  AND deleted_at IS NULL
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountAutopilots :one
SELECT count(*) FROM autopilot
WHERE workspace_id = $1
  AND deleted_at IS NULL
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'));

-- name: GetAutopilotInWorkspace :one
SELECT * FROM autopilot
WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL;

-- name: CreateAutopilot :one
INSERT INTO autopilot (
    workspace_id, title, description, status, mode, agent_id,
    project_id, priority, issue_title_template, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
) RETURNING *;

-- name: UpdateAutopilot :one
UPDATE autopilot SET
    title = COALESCE(sqlc.narg('title'), title),
    description = CASE WHEN @set_description::boolean THEN sqlc.narg('description') ELSE description END,
    status = COALESCE(sqlc.narg('status'), status),
    mode = COALESCE(sqlc.narg('mode'), mode),
    agent_id = COALESCE(sqlc.narg('agent_id'), agent_id),
    project_id = CASE WHEN @set_project_id::boolean THEN sqlc.narg('project_id') ELSE project_id END,
    priority = COALESCE(sqlc.narg('priority'), priority),
    issue_title_template = COALESCE(sqlc.narg('issue_title_template'), issue_title_template),
    updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id AND deleted_at IS NULL
RETURNING *;

-- name: DeleteAutopilot :one
UPDATE autopilot
SET deleted_at = now(), updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL
RETURNING *;

-- name: ListAutopilotTriggers :many
SELECT * FROM autopilot_trigger
WHERE autopilot_id = $1
ORDER BY created_at ASC;

-- name: GetAutopilotTriggerInWorkspace :one
SELECT t.* FROM autopilot_trigger t
JOIN autopilot a ON a.id = t.autopilot_id
WHERE t.id = $1
  AND t.autopilot_id = $2
  AND a.workspace_id = $3
  AND a.deleted_at IS NULL;

-- name: CreateAutopilotTrigger :one
INSERT INTO autopilot_trigger (
    autopilot_id, type, label, cron, timezone, status, next_run_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
) RETURNING *;

-- name: UpdateAutopilotTrigger :one
UPDATE autopilot_trigger SET
    label = CASE WHEN @set_label::boolean THEN sqlc.narg('label') ELSE label END,
    cron = COALESCE(sqlc.narg('cron'), cron),
    timezone = COALESCE(sqlc.narg('timezone'), timezone),
    status = COALESCE(sqlc.narg('status'), status),
    next_run_at = CASE WHEN @set_next_run_at::boolean THEN sqlc.narg('next_run_at') ELSE next_run_at END,
    updated_at = now()
WHERE autopilot_trigger.id = @id
  AND autopilot_trigger.autopilot_id = @autopilot_id
  AND EXISTS (
      SELECT 1 FROM autopilot a
      WHERE a.id = autopilot_trigger.autopilot_id
        AND a.workspace_id = @workspace_id
        AND a.deleted_at IS NULL
  )
RETURNING *;

-- name: DeleteAutopilotTrigger :one
DELETE FROM autopilot_trigger
WHERE autopilot_trigger.id = $1
  AND autopilot_trigger.autopilot_id = $2
  AND EXISTS (
      SELECT 1 FROM autopilot a
      WHERE a.id = autopilot_trigger.autopilot_id
        AND a.workspace_id = $3
        AND a.deleted_at IS NULL
  )
RETURNING *;

-- name: ClaimDueAutopilotSchedules :many
SELECT
    a.id AS autopilot_id,
    a.workspace_id AS autopilot_workspace_id,
    a.title AS autopilot_title,
    a.description AS autopilot_description,
    a.status AS autopilot_status,
    a.mode AS autopilot_mode,
    a.agent_id AS autopilot_agent_id,
    a.project_id AS autopilot_project_id,
    a.priority AS autopilot_priority,
    a.issue_title_template AS autopilot_issue_title_template,
    a.created_by AS autopilot_created_by,
    a.created_at AS autopilot_created_at,
    a.updated_at AS autopilot_updated_at,
    a.deleted_at AS autopilot_deleted_at,
    t.id AS trigger_id,
    t.autopilot_id AS trigger_autopilot_id,
    t.type AS trigger_type,
    t.label AS trigger_label,
    t.cron AS trigger_cron,
    t.timezone AS trigger_timezone,
    t.status AS trigger_status,
    t.next_run_at AS trigger_next_run_at,
    t.last_run_at AS trigger_last_run_at,
    t.created_at AS trigger_created_at,
    t.updated_at AS trigger_updated_at
FROM autopilot_trigger t
JOIN autopilot a ON a.id = t.autopilot_id
WHERE t.type = 'schedule'
  AND t.status = 'active'
  AND t.next_run_at IS NOT NULL
  AND t.next_run_at <= @now
  AND a.status = 'active'
  AND a.mode = 'create_issue'
  AND a.deleted_at IS NULL
ORDER BY t.next_run_at ASC
LIMIT @claim_limit
FOR UPDATE OF t SKIP LOCKED;

-- name: AdvanceAutopilotTrigger :one
UPDATE autopilot_trigger
SET last_run_at = $2,
    next_run_at = $3,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CreateAutopilotRun :one
INSERT INTO autopilot_run (
    workspace_id, autopilot_id, trigger_id, source, status,
    scheduled_for, started_at, idempotency_key
) VALUES (
    $1, $2, $3, $4, 'running', $5, now(), $6
)
ON CONFLICT (autopilot_id, trigger_id, idempotency_key)
WHERE idempotency_key IS NOT NULL
DO NOTHING
RETURNING *;

-- name: ListAutopilotRuns :many
SELECT * FROM autopilot_run
WHERE workspace_id = $1 AND autopilot_id = $2
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: CountAutopilotRuns :one
SELECT count(*) FROM autopilot_run
WHERE workspace_id = $1 AND autopilot_id = $2;

-- name: CompleteAutopilotRunSucceeded :one
UPDATE autopilot_run
SET status = 'succeeded',
    completed_at = now(),
    created_issue_id = $2,
    created_task_id = $3,
    error = NULL
WHERE id = $1
RETURNING *;

-- name: CompleteAutopilotRunFailed :one
UPDATE autopilot_run
SET status = 'failed',
    completed_at = now(),
    created_issue_id = COALESCE(sqlc.narg('created_issue_id'), created_issue_id),
    error = $2
WHERE id = $1
RETURNING *;

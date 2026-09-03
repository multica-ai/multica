-- name: LockWorkflowDefinitionVersionKey :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg('workspace_id')::text || ':' || lower(btrim(sqlc.arg('name')::text)), 0));

-- name: GetLatestWorkflowDefinitionVersionByName :one
SELECT * FROM workflow_definition
WHERE workspace_id = sqlc.arg('workspace_id')
  AND lower(name) = lower(sqlc.arg('name'))
ORDER BY version DESC
LIMIT 1;

-- name: CreateWorkflowDefinition :one
INSERT INTO workflow_definition (id, workspace_id, name, version, definition, created_by)
VALUES (sqlc.arg('id'), sqlc.arg('workspace_id'), sqlc.arg('name'), sqlc.arg('version'), sqlc.arg('definition'), sqlc.arg('created_by'))
RETURNING *;

-- name: GetWorkflowDefinitionInWorkspace :one
SELECT * FROM workflow_definition
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id');

-- name: ListLatestWorkflowDefinitions :many
SELECT DISTINCT ON (lower(name)) * FROM workflow_definition
WHERE workspace_id = sqlc.arg('workspace_id')
ORDER BY lower(name), version DESC, created_at DESC;

-- name: CreateWorkflowRun :one
INSERT INTO workflow_run (
    id, workspace_id, issue_id, workflow_definition_id, definition_snapshot,
    status, current_stage, started_by_type, started_by_id
) VALUES (
    sqlc.arg('id'), sqlc.arg('workspace_id'), sqlc.arg('issue_id'),
    sqlc.arg('workflow_definition_id'), sqlc.arg('definition_snapshot'),
    sqlc.arg('status'), sqlc.arg('current_stage'), sqlc.arg('started_by_type'),
    sqlc.arg('started_by_id')
)
RETURNING *;

-- name: GetActiveWorkflowRunForIssue :one
SELECT * FROM workflow_run
WHERE workspace_id = sqlc.arg('workspace_id')
  AND issue_id = sqlc.arg('issue_id')
  AND status IN ('running', 'blocked_materialization')
ORDER BY created_at DESC
LIMIT 1;

-- name: GetLatestWorkflowRunForIssue :one
SELECT * FROM workflow_run
WHERE workspace_id = sqlc.arg('workspace_id') AND issue_id = sqlc.arg('issue_id')
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: LockActiveWorkflowRunForIssue :one
SELECT * FROM workflow_run
WHERE workspace_id = sqlc.arg('workspace_id')
  AND issue_id = sqlc.arg('issue_id')
  AND status IN ('running', 'blocked_materialization')
ORDER BY created_at DESC
LIMIT 1
FOR UPDATE;

-- name: UpdateWorkflowRun :one
UPDATE workflow_run SET
    status = sqlc.arg('status'),
    current_stage = sqlc.arg('current_stage'),
    revision = revision + 1,
    completed_at = COALESCE(sqlc.narg('completed_at')::timestamptz, completed_at),
    cancelled_at = COALESCE(sqlc.narg('cancelled_at')::timestamptz, cancelled_at),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND revision = sqlc.arg('expected_revision')
RETURNING *;

-- name: CreateWorkflowTransition :one
INSERT INTO workflow_transition (
    id, workspace_id, workflow_run_id, idempotency_key, kind,
    from_stage, to_stage, from_status, to_status, actor_type, actor_id, payload
) VALUES (
    sqlc.arg('id'), sqlc.arg('workspace_id'), sqlc.arg('workflow_run_id'),
    sqlc.arg('idempotency_key'), sqlc.arg('kind'), sqlc.narg('from_stage'),
    sqlc.narg('to_stage'), sqlc.narg('from_status'), sqlc.arg('to_status'),
    sqlc.arg('actor_type'), sqlc.narg('actor_id'), sqlc.arg('payload')
)
RETURNING *;

-- name: ListWorkflowTransitions :many
SELECT * FROM workflow_transition
WHERE workspace_id = sqlc.arg('workspace_id')
  AND workflow_run_id = sqlc.arg('workflow_run_id')
ORDER BY created_at ASC, id ASC;

-- name: LockWorkflowParent :one
SELECT * FROM issue
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
FOR UPDATE;

-- name: LockWorkflowChildren :many
SELECT * FROM issue
WHERE workspace_id = sqlc.arg('workspace_id')
  AND parent_issue_id = sqlc.arg('parent_issue_id')
ORDER BY number ASC
FOR UPDATE;

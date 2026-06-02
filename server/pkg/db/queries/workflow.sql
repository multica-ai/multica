-- =====================
-- Workflow CRUD
-- =====================

-- name: ListWorkflows :many
SELECT * FROM workflow
WHERE workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetWorkflow :one
SELECT * FROM workflow
WHERE id = $1;

-- name: GetWorkflowInWorkspace :one
SELECT * FROM workflow
WHERE id = $1 AND workspace_id = $2;

-- name: CountWorkflowNodes :one
SELECT count(*)::bigint FROM workflow_node
WHERE workflow_id = $1;

-- name: CreateWorkflow :one
INSERT INTO workflow (
    workspace_id, title, description, status, max_retries,
    created_by_type, created_by_id
) VALUES (
    $1, $2, sqlc.narg('description'), $3, $4, $5, $6
) RETURNING *;

-- name: UpdateWorkflow :one
UPDATE workflow SET
    title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    status = COALESCE(sqlc.narg('status'), status),
    max_retries = COALESCE(sqlc.narg('max_retries')::int, max_retries),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteWorkflow :exec
DELETE FROM workflow WHERE id = $1;

-- =====================
-- Workflow Node CRUD
-- =====================

-- name: ListWorkflowNodes :many
SELECT * FROM workflow_node
WHERE workflow_id = $1
ORDER BY sort_order ASC, created_at ASC;

-- name: GetWorkflowNode :one
SELECT * FROM workflow_node
WHERE id = $1;

-- name: CreateWorkflowNode :one
INSERT INTO workflow_node (
    workflow_id, title, description, position_x, position_y,
    format_schema, worker_type, worker_id, worker_instructions,
    critic_type, critic_id, critic_instructions, critic_api_url,
    sort_order
) VALUES (
    $1, $2, sqlc.narg('description'), $3, $4,
    sqlc.narg('format_schema'), $5, sqlc.narg('worker_id'), sqlc.narg('worker_instructions'),
    $6, sqlc.narg('critic_id'), sqlc.narg('critic_instructions'), sqlc.narg('critic_api_url'),
    $7
) RETURNING *;

-- name: UpdateWorkflowNode :one
UPDATE workflow_node SET
    title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    position_x = COALESCE(sqlc.narg('position_x')::float, position_x),
    position_y = COALESCE(sqlc.narg('position_y')::float, position_y),
    format_schema = COALESCE(sqlc.narg('format_schema'), format_schema),
    worker_type = COALESCE(sqlc.narg('worker_type'), worker_type),
    worker_id = COALESCE(sqlc.narg('worker_id'), worker_id),
    worker_instructions = COALESCE(sqlc.narg('worker_instructions'), worker_instructions),
    critic_type = COALESCE(sqlc.narg('critic_type'), critic_type),
    critic_id = COALESCE(sqlc.narg('critic_id'), critic_id),
    critic_instructions = COALESCE(sqlc.narg('critic_instructions'), critic_instructions),
    critic_api_url = COALESCE(sqlc.narg('critic_api_url'), critic_api_url),
    sort_order = COALESCE(sqlc.narg('sort_order')::int, sort_order),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteWorkflowNode :exec
DELETE FROM workflow_node WHERE id = $1;

-- name: DeleteWorkflowNodesByWorkflow :exec
DELETE FROM workflow_node WHERE workflow_id = $1;

-- =====================
-- Workflow Edge CRUD
-- =====================

-- name: ListWorkflowEdges :many
SELECT * FROM workflow_edge
WHERE workflow_id = $1
ORDER BY created_at ASC;

-- name: GetWorkflowEdge :one
SELECT * FROM workflow_edge
WHERE id = $1;

-- name: CreateWorkflowEdge :one
INSERT INTO workflow_edge (
    workflow_id, source_node_id, target_node_id, condition
) VALUES (
    $1, $2, $3, sqlc.narg('condition')
) RETURNING *;

-- name: DeleteWorkflowEdge :exec
DELETE FROM workflow_edge WHERE id = $1;

-- name: DeleteWorkflowEdgesByWorkflow :exec
DELETE FROM workflow_edge WHERE workflow_id = $1;

-- name: ListWorkflowEdgesBySource :many
SELECT * FROM workflow_edge
WHERE source_node_id = $1
ORDER BY created_at ASC;

-- name: ListWorkflowEdgesByTarget :many
SELECT * FROM workflow_edge
WHERE target_node_id = $1
ORDER BY created_at ASC;

-- =====================
-- Workflow Run CRUD
-- =====================

-- name: ListWorkflowRuns :many
SELECT * FROM workflow_run
WHERE workflow_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListWorkflowRunsByWorkspace :many
SELECT * FROM workflow_run
WHERE workspace_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetWorkflowRun :one
SELECT * FROM workflow_run
WHERE id = $1;

-- name: CreateWorkflowRun :one
INSERT INTO workflow_run (
    workflow_id, workspace_id, workflow_title, status,
    triggered_by_type, triggered_by_id, input
) VALUES (
    $1, $2, $3, $4, $5, sqlc.narg('triggered_by_id'), sqlc.narg('input')
) RETURNING *;

-- name: UpdateWorkflowRunStatus :one
UPDATE workflow_run SET
    status = $2,
    completed_at = CASE WHEN $2 IN ('completed', 'failed', 'cancelled') THEN now() ELSE completed_at END
WHERE id = $1
RETURNING *;

-- name: CompleteWorkflowRun :one
UPDATE workflow_run SET
    status = 'completed',
    output = sqlc.narg('output'),
    completed_at = now()
WHERE id = $1
RETURNING *;

-- name: FailWorkflowRun :one
UPDATE workflow_run SET
    status = 'failed',
    completed_at = now()
WHERE id = $1
RETURNING *;

-- name: CancelWorkflowRun :one
UPDATE workflow_run SET
    status = 'cancelled',
    completed_at = now()
WHERE id = $1
RETURNING *;

-- =====================
-- Template queries
-- =====================

-- name: ListTemplates :many
SELECT * FROM workflow
WHERE is_template = TRUE
ORDER BY created_at DESC;

-- name: ListWorkflowsExcludingTemplates :many
SELECT * FROM workflow
WHERE workspace_id = $1 AND is_template = FALSE
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: SetWorkflowTemplate :one
UPDATE workflow SET
    is_template = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CountWorkflowsBySourceTemplate :one
SELECT count(*)::bigint FROM workflow
WHERE source_template_id = $1;

-- name: CreateWorkflowFromTemplate :one
INSERT INTO workflow (
    workspace_id, title, description, status, max_retries,
    created_by_type, created_by_id, is_template, source_template_id
) VALUES (
    $1, $2, sqlc.narg('description'), $3, $4, $5, $6, FALSE, $7
) RETURNING *;

-- =====================
-- Workflow admin management
-- =====================

-- name: ListWorkflowAdminUsers :many
SELECT * FROM "user"
WHERE can_manage_workflows = TRUE
ORDER BY name ASC;

-- name: SetUserWorkflowAdmin :one
UPDATE "user" SET
    can_manage_workflows = $2
WHERE id = $1
RETURNING *;

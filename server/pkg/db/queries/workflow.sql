-- =====================
-- Workflow CRUD
-- =====================

-- name: ListWorkflows :many
SELECT * FROM multica_workflow
WHERE workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetWorkflow :one
SELECT * FROM multica_workflow
WHERE id = $1;

-- name: GetWorkflowInWorkspace :one
SELECT * FROM multica_workflow
WHERE id = $1 AND workspace_id = $2;

-- name: CountWorkflowNodes :one
SELECT count(*)::bigint FROM multica_workflow_node
WHERE workflow_id = $1;

-- name: CreateWorkflow :one
INSERT INTO multica_workflow (
    workspace_id, title, description, status, max_retries,
    created_by_type, created_by_id
) VALUES (
    $1, $2, sqlc.narg('description'), $3, $4, $5, $6
) RETURNING *;

-- name: UpdateWorkflow :one
UPDATE multica_workflow SET
    title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    status = COALESCE(sqlc.narg('status'), status),
    max_retries = COALESCE(sqlc.narg('max_retries')::int, max_retries),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteWorkflow :exec
DELETE FROM multica_workflow WHERE id = $1;

-- =====================
-- Workflow Node CRUD
-- =====================

-- name: ListWorkflowNodes :many
SELECT * FROM multica_workflow_node
WHERE workflow_id = $1
ORDER BY sort_order ASC, created_at ASC;

-- name: GetWorkflowNode :one
SELECT * FROM multica_workflow_node
WHERE id = $1;

-- name: CreateWorkflowNode :one
INSERT INTO multica_workflow_node (
    workflow_id, title, description, position_x, position_y,
    format_schema, worker_type, worker_id,
    critic_type, critic_id, critic_api_url,
    sort_order
) VALUES (
    $1, $2, sqlc.narg('description'), $3, $4,
    sqlc.narg('format_schema'), $5, sqlc.narg('worker_id'),
    $6, sqlc.narg('critic_id'), sqlc.narg('critic_api_url'),
    $7
) RETURNING *;

-- name: UpdateWorkflowNode :one
UPDATE multica_workflow_node SET
    title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    position_x = COALESCE(sqlc.narg('position_x')::float, position_x),
    position_y = COALESCE(sqlc.narg('position_y')::float, position_y),
    format_schema = COALESCE(sqlc.narg('format_schema'), format_schema),
    worker_type = COALESCE(sqlc.narg('worker_type'), worker_type),
    worker_id = COALESCE(sqlc.narg('worker_id'), worker_id),
    critic_type = COALESCE(sqlc.narg('critic_type'), critic_type),
    critic_id = COALESCE(sqlc.narg('critic_id'), critic_id),
    critic_api_url = COALESCE(sqlc.narg('critic_api_url'), critic_api_url),
    sort_order = COALESCE(sqlc.narg('sort_order')::int, sort_order),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteWorkflowNode :exec
DELETE FROM multica_workflow_node WHERE id = $1;

-- name: DeleteWorkflowNodesByWorkflow :exec
DELETE FROM multica_workflow_node WHERE workflow_id = $1;

-- =====================
-- Workflow Edge CRUD
-- =====================

-- name: ListWorkflowEdges :many
SELECT * FROM multica_workflow_edge
WHERE workflow_id = $1
ORDER BY created_at ASC;

-- name: GetWorkflowEdge :one
SELECT * FROM multica_workflow_edge
WHERE id = $1;

-- name: CreateWorkflowEdge :one
INSERT INTO multica_workflow_edge (
    workflow_id, source_node_id, target_node_id, condition
) VALUES (
    $1, $2, $3, sqlc.narg('condition')
) RETURNING *;

-- name: DeleteWorkflowEdge :exec
DELETE FROM multica_workflow_edge WHERE id = $1;

-- name: DeleteWorkflowEdgesByWorkflow :exec
DELETE FROM multica_workflow_edge WHERE workflow_id = $1;

-- name: ListWorkflowEdgesBySource :many
SELECT * FROM multica_workflow_edge
WHERE source_node_id = $1
ORDER BY created_at ASC;

-- name: ListWorkflowEdgesByTarget :many
SELECT * FROM multica_workflow_edge
WHERE target_node_id = $1
ORDER BY created_at ASC;

-- =====================
-- Workflow Run CRUD
-- =====================

-- name: ListWorkflowRuns :many
SELECT * FROM multica_workflow_run
WHERE workflow_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListWorkflowRunsByWorkspace :many
SELECT * FROM multica_workflow_run
WHERE workspace_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetWorkflowRun :one
SELECT * FROM multica_workflow_run
WHERE id = $1;

-- name: CreateWorkflowRun :one
INSERT INTO multica_workflow_run (
    workflow_id, workspace_id, workflow_title, status,
    triggered_by_type, triggered_by_id, input, runtime_id
) VALUES (
    $1, $2, $3, $4, $5, sqlc.narg('triggered_by_id'), sqlc.narg('input'), sqlc.narg('runtime_id')
) RETURNING *;

-- name: UpdateWorkflowRunStatus :one
UPDATE multica_workflow_run SET
    status = $2,
    completed_at = CASE WHEN $2 IN ('completed', 'failed', 'cancelled') THEN now() ELSE completed_at END
WHERE id = $1
RETURNING *;

-- name: CompleteWorkflowRun :one
UPDATE multica_workflow_run SET
    status = 'completed',
    output = sqlc.narg('output'),
    completed_at = now()
WHERE id = $1
RETURNING *;

-- name: FailWorkflowRun :one
UPDATE multica_workflow_run SET
    status = 'failed',
    completed_at = now()
WHERE id = $1
RETURNING *;

-- name: CancelWorkflowRun :one
UPDATE multica_workflow_run SET
    status = 'cancelled',
    completed_at = now()
WHERE id = $1
RETURNING *;

-- =====================
-- Template queries
-- =====================

-- name: ListTemplates :many
SELECT * FROM multica_workflow
WHERE is_template = TRUE
ORDER BY created_at DESC;

-- name: ListWorkflowsExcludingTemplates :many
SELECT * FROM multica_workflow
WHERE workspace_id = $1 AND is_template = FALSE
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: SetWorkflowTemplate :one
UPDATE multica_workflow SET
    is_template = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CountWorkflowsBySourceTemplate :one
SELECT count(*)::bigint FROM multica_workflow
WHERE source_template_id = $1;

-- name: CreateWorkflowFromTemplate :one
INSERT INTO multica_workflow (
    workspace_id, title, description, status, max_retries,
    created_by_type, created_by_id, is_template, source_template_id
) VALUES (
    $1, $2, sqlc.narg('description'), $3, $4, $5, $6, FALSE, $7
) RETURNING *;

-- =====================
-- Workflow admin management
-- =====================

-- name: ListWorkflowAdminUsers :many
SELECT * FROM multica_user
WHERE can_manage_workflows = TRUE
ORDER BY name ASC;

-- name: SetUserWorkflowAdmin :one
UPDATE multica_user SET
    can_manage_workflows = $2
WHERE id = $1
RETURNING *;

-- =====================
-- Workflow Stage CRUD
-- =====================

-- name: CreateWorkflowStage :one
INSERT INTO multica_workflow_stage (
    workflow_id, name, description, sort_order
) VALUES (
    $1, $2, sqlc.narg('description'), $3
) RETURNING *;

-- name: GetWorkflowStage :one
SELECT * FROM multica_workflow_stage WHERE id = $1;

-- name: ListWorkflowStagesByWorkflow :many
SELECT * FROM multica_workflow_stage
WHERE workflow_id = $1
ORDER BY sort_order ASC, created_at ASC;

-- name: UpdateWorkflowStage :one
UPDATE multica_workflow_stage SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    sort_order = COALESCE(sqlc.narg('sort_order')::int, sort_order),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteWorkflowStage :exec
DELETE FROM multica_workflow_stage WHERE id = $1;

-- name: CountWorkflowStageNodes :one
SELECT count(*)::bigint FROM multica_workflow_node
WHERE stage_id = $1;

-- name: AssignNodeToStage :one
UPDATE multica_workflow_node SET
    stage_id = sqlc.narg('stage_id'),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UnassignNodeFromStage :one
UPDATE multica_workflow_node SET
    stage_id = NULL,
    updated_at = now()
WHERE id = $1
RETURNING *;

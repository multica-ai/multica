-- name: CreateWorkflow :one
INSERT INTO workflow (plan_id, title, status)
VALUES ($1, $2, 'draft')
RETURNING *;

-- name: GetWorkflow :one
SELECT * FROM workflow WHERE id = $1;

-- name: GetWorkflowByPlan :one
SELECT * FROM workflow WHERE plan_id = $1;

-- name: UpdateWorkflow :one
UPDATE workflow SET
    title = COALESCE(sqlc.narg('title'), title),
    status = COALESCE(sqlc.narg('status'), status),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CreateWorkflowNode :one
INSERT INTO workflow_node (workflow_id, agent_id, title, prompt, position_x, position_y, status)
VALUES ($1, $2, $3, $4, $5, $6, 'pending')
RETURNING *;

-- name: ListWorkflowNodes :many
SELECT * FROM workflow_node WHERE workflow_id = $1 ORDER BY created_at ASC;

-- name: UpdateWorkflowNode :one
UPDATE workflow_node SET
    title = COALESCE(sqlc.narg('title'), title),
    prompt = COALESCE(sqlc.narg('prompt'), prompt),
    agent_id = COALESCE(sqlc.narg('agent_id'), agent_id),
    position_x = COALESCE(sqlc.narg('position_x'), position_x),
    position_y = COALESCE(sqlc.narg('position_y'), position_y),
    status = COALESCE(sqlc.narg('status'), status),
    task_id = COALESCE(sqlc.narg('task_id'), task_id),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteWorkflowNode :exec
DELETE FROM workflow_node WHERE id = $1;

-- name: CreateWorkflowEdge :one
INSERT INTO workflow_edge (workflow_id, source_node_id, target_node_id)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListWorkflowEdges :many
SELECT * FROM workflow_edge WHERE workflow_id = $1;

-- name: DeleteWorkflowEdge :exec
DELETE FROM workflow_edge WHERE id = $1;

-- name: GetNodeByTaskID :one
SELECT * FROM workflow_node WHERE task_id = $1;
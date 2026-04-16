-- name: ListWorkflows :many
SELECT * FROM workflow
WHERE workspace_id = $1 AND archived_at IS NULL
ORDER BY created_at DESC;

-- name: GetWorkflow :one
SELECT * FROM workflow
WHERE id = $1;

-- name: GetWorkflowInWorkspace :one
SELECT * FROM workflow
WHERE id = $1 AND workspace_id = $2 AND archived_at IS NULL;

-- name: CreateWorkflow :one
INSERT INTO workflow (workspace_id, name, description, steps, created_by, mode, planner_agent_id)
VALUES ($1, $2, $3, $4, $5, $6, sqlc.narg(planner_agent_id))
RETURNING *;

-- name: UpdateWorkflow :one
UPDATE workflow SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    steps = COALESCE(sqlc.narg('steps'), steps),
    mode = COALESCE(sqlc.narg('mode'), mode),
    planner_agent_id = CASE WHEN sqlc.narg('planner_agent_id')::UUID IS NOT NULL THEN sqlc.narg('planner_agent_id')::UUID ELSE planner_agent_id END,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ArchiveWorkflow :one
UPDATE workflow SET archived_at = now(), updated_at = now()
WHERE id = $1
RETURNING *;

-- name: RestoreWorkflow :one
UPDATE workflow SET archived_at = NULL, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CreateWorkflowRun :one
INSERT INTO workflow_run (workflow_id, workspace_id, trigger_type, trigger_id, status, current_step_index, issue_id, context)
VALUES ($1, $2, $3, sqlc.narg(trigger_id), $4, $5, sqlc.narg(issue_id), $6)
RETURNING *;

-- name: GetWorkflowRun :one
SELECT * FROM workflow_run
WHERE id = $1;

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

-- name: UpdateWorkflowRunStatus :one
UPDATE workflow_run SET
    status = $2,
    current_step_index = $3,
    completed_at = CASE WHEN $2 IN ('completed', 'failed', 'cancelled') THEN now() ELSE completed_at END,
    started_at = CASE WHEN $2 = 'running' AND started_at IS NULL THEN now() ELSE started_at END
WHERE id = $1
RETURNING *;

-- name: CancelWorkflowRun :one
UPDATE workflow_run SET status = 'cancelled', completed_at = now()
WHERE id = $1 AND status IN ('pending', 'running', 'paused')
RETURNING *;

-- name: CreateWorkflowStepRun :one
INSERT INTO workflow_step_run (workflow_run_id, step_index, step_type, status)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetWorkflowStepRun :one
SELECT * FROM workflow_step_run
WHERE id = $1;

-- name: ListWorkflowStepRuns :many
SELECT * FROM workflow_step_run
WHERE workflow_run_id = $1
ORDER BY step_index ASC;

-- name: UpdateWorkflowStepRunStatus :one
UPDATE workflow_step_run SET
    status = $2,
    started_at = CASE WHEN $2 = 'running' AND started_at IS NULL THEN now() ELSE started_at END,
    completed_at = CASE WHEN $2 IN ('completed', 'failed', 'skipped', 'rejected') THEN now() ELSE completed_at END
WHERE id = $1
RETURNING *;

-- name: SetWorkflowStepRunAgentTask :one
UPDATE workflow_step_run SET agent_task_id = $2
WHERE id = $1
RETURNING *;

-- name: SetWorkflowStepRunReviewer :one
UPDATE workflow_step_run SET reviewer_id = $2
WHERE id = $1
RETURNING *;

-- name: UpdateStepRunDecision :one
UPDATE workflow_step_run SET
    decision = $2,
    decision_comment = sqlc.narg(decision_comment),
    redirect_to_step = sqlc.narg(redirect_to_step),
    status = CASE
        WHEN $2 = 'approved' THEN 'completed'
        WHEN $2 = 'rejected' THEN 'rejected'
        WHEN $2 = 'stopped' THEN 'failed'
        WHEN $2 = 'redirect' THEN 'completed'
        ELSE status
    END,
    completed_at = now()
WHERE id = $1 AND step_type IN ('approval', 'review') AND status = 'running'
RETURNING *;

-- name: GetStepRunByAgentTaskID :one
SELECT * FROM workflow_step_run
WHERE agent_task_id = $1;

-- name: GetPendingApprovalSteps :many
SELECT wsr.*, wr.workspace_id, wr.workflow_id
FROM workflow_step_run wsr
JOIN workflow_run wr ON wr.id = wsr.workflow_run_id
WHERE wsr.step_type IN ('approval', 'review')
  AND wsr.status = 'running'
  AND wr.workspace_id = $1
ORDER BY wsr.created_at ASC;

-- name: GetPendingApprovalStepsByReviewer :many
SELECT wsr.*, wr.workspace_id, wr.workflow_id
FROM workflow_step_run wsr
JOIN workflow_run wr ON wr.id = wsr.workflow_run_id
WHERE wsr.step_type IN ('approval', 'review')
  AND wsr.status = 'running'
  AND wsr.reviewer_id = $1
  AND wr.workspace_id = $2
ORDER BY wsr.created_at ASC;

-- name: SetWorkflowRunIssue :one
UPDATE workflow_run SET issue_id = $2, context = $3
WHERE id = $1
RETURNING *;

-- name: CountWorkflowRuns :one
SELECT count(*) FROM workflow_run
WHERE workflow_id = $1;

-- name: SetWorkflowRunDynamicSteps :one
UPDATE workflow_run SET dynamic_steps = $2
WHERE id = $1
RETURNING *;

-- name: SetWorkflowRunPlannerTask :one
UPDATE workflow_run SET planner_task_id = $2
WHERE id = $1
RETURNING *;

-- name: GetWorkflowRunByPlannerTask :one
SELECT * FROM workflow_run
WHERE planner_task_id = $1;

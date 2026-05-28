-- =====================
-- Workflow Node Run State Machine
-- =====================

-- name: ListWorkflowNodeRuns :many
SELECT * FROM multica_workflow_node_run
WHERE workflow_run_id = $1
ORDER BY created_at ASC;

-- name: ListWorkflowNodeRunsByRun :many
SELECT * FROM multica_workflow_node_run
WHERE workflow_run_id = $1
ORDER BY created_at ASC;

-- name: ListWorkflowNodeRunsByRunAndNode :one
SELECT * FROM multica_workflow_node_run
WHERE workflow_run_id = $1 AND workflow_node_id = $2
LIMIT 1;

-- name: GetWorkflowNodeRun :one
SELECT * FROM multica_workflow_node_run
WHERE id = $1;

-- name: CreateWorkflowNodeRun :one
INSERT INTO multica_workflow_node_run (
    workflow_run_id, workflow_node_id, node_title, status,
    retry_count, worker_type, worker_id, critic_type, critic_id
) VALUES (
    $1, $2, $3, $4, $5, $6, sqlc.narg('worker_id'), $7, sqlc.narg('critic_id')
) RETURNING *;

-- name: UpdateWorkflowNodeRunStatus :one
UPDATE multica_workflow_node_run SET
    status = $2,
    started_at = CASE
        WHEN $2 IN ('format_checking', 'working', 'critic_reviewing')
             AND started_at IS NULL THEN now()
        ELSE started_at
    END,
    completed_at = CASE
        WHEN $2 IN ('format_failed', 'completed', 'failed', 'blocked', 'skipped', 'cancelled')
             THEN now()
        ELSE completed_at
    END,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateWorkflowNodeRunWorkerOutput :one
UPDATE multica_workflow_node_run SET
    worker_output = $2,
    status = 'awaiting_critic',
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetWorkflowNodeRunWorkerOutput :one
UPDATE multica_workflow_node_run SET
    worker_output = $2,
    status = $3,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateWorkflowNodeRunCriticReview :one
UPDATE multica_workflow_node_run SET
    critic_output = sqlc.narg('critic_output'),
    critic_comment = sqlc.narg('critic_comment'),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetWorkflowNodeRunCriticOutput :one
UPDATE multica_workflow_node_run SET
    critic_output = sqlc.narg('critic_output'),
    critic_comment = sqlc.narg('critic_comment'),
    status = $2,
    retry_count = COALESCE(sqlc.narg('retry_count')::int, retry_count),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateWorkflowNodeRunRework :one
UPDATE multica_workflow_node_run SET
    status = $2,
    retry_count = retry_count + 1,
    worker_output = NULL,
    critic_output = NULL,
    critic_comment = '',
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateWorkflowNodeRunAgentTask :one
UPDATE multica_workflow_node_run SET
    agent_task_id = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: LinkNodeRunWorkerTask :one
UPDATE multica_workflow_node_run SET
    worker_agent_task_id = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: LinkNodeRunCriticTask :one
UPDATE multica_workflow_node_run SET
    critic_agent_task_id = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: LinkNodeRunAgentTask :one
UPDATE multica_workflow_node_run SET
    agent_task_id = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CancelWorkflowNodeRuns :exec
UPDATE multica_workflow_node_run SET
    status = 'cancelled',
    completed_at = now(),
    updated_at = now()
WHERE workflow_run_id = $1
  AND status NOT IN ('format_failed', 'completed', 'failed', 'blocked', 'skipped', 'cancelled');

-- name: GetWorkflowNodeRunsByStatus :many
SELECT * FROM multica_workflow_node_run
WHERE workflow_run_id = $1 AND status = $2
ORDER BY created_at ASC;

-- name: GetDownstreamNodeRuns :many
-- Returns node runs whose node has an incoming edge from the given node.
-- Used to find downstream nodes that should be activated when an upstream completes.
SELECT wnr.*
FROM multica_workflow_node_run wnr
JOIN multica_workflow_edge we ON we.target_node_id = wnr.workflow_node_id
WHERE we.source_node_id = sqlc.arg('workflow_node_id')
  AND wnr.workflow_run_id = $1;

-- name: GetNodeRunUpstreamStatuses :many
-- For a given node run, returns the status of all upstream node runs.
-- Used to check if all upstreams are complete before activating a node.
SELECT up_wnr.status
FROM multica_workflow_node_run wnr
JOIN multica_workflow_edge we ON we.target_node_id = wnr.workflow_node_id
JOIN multica_workflow_node_run up_wnr ON up_wnr.workflow_node_id = we.source_node_id
    AND up_wnr.workflow_run_id = wnr.workflow_run_id
WHERE wnr.id = $1;

-- name: ListActiveNodeRuns :many
-- Returns all active (non-terminal) node runs for a multica_workflow run.
SELECT * FROM multica_workflow_node_run
WHERE workflow_run_id = $1
  AND status NOT IN ('format_failed', 'completed', 'failed', 'blocked', 'skipped', 'cancelled');

-- name: ListMyWorkflowTasks :many
-- Returns node runs assigned to the current user as human worker or critic.
SELECT wnr.*,
       wr.workflow_title,
       wr.workflow_id,
       wr.workspace_id
FROM multica_workflow_node_run wnr
JOIN multica_workflow_run wr ON wr.id = wnr.workflow_run_id
WHERE wr.workspace_id = $1
  AND (
    -- Human worker: node run is assigned to this multica_member via worker_id
    (wnr.worker_type = 'human' AND wnr.worker_id = sqlc.narg('member_id')::uuid AND wnr.status IN ('worker_assigned', 'working'))
    -- Human critic: node run is assigned to this multica_member via critic_id
    OR (wnr.critic_type = 'human' AND wnr.critic_id = sqlc.narg('member_id')::uuid AND wnr.status = 'awaiting_critic')
    -- Any human worker (worker_type=human, worker_id is null): anyone can claim
    OR (wnr.worker_type = 'human' AND wnr.worker_id IS NULL AND wnr.status = 'worker_assigned')
    -- Any human critic (critic_type=human, critic_id is null): anyone can claim
    OR (wnr.critic_type = 'human' AND wnr.critic_id IS NULL AND wnr.status = 'awaiting_critic')
  )
  AND wr.status = 'running'
ORDER BY wnr.created_at DESC
LIMIT $2 OFFSET $3;

-- name: CreateWorkflowAgentTask :one
INSERT INTO multica_agent_task_queue (agent_id, runtime_id, issue_id, status, priority, workflow_node_run_id, context)
VALUES ($1, $2, sqlc.narg('issue_id'), 'queued', $3, sqlc.narg('workflow_node_run_id'), sqlc.narg('context'))
RETURNING *;

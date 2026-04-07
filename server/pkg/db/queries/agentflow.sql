-- name: CreateAgentflow :one
INSERT INTO agentflow (
    workspace_id, title, description, agent_id, status,
    concurrency_policy, variables, created_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetAgentflow :one
SELECT * FROM agentflow WHERE id = $1;

-- name: GetAgentflowInWorkspace :one
SELECT * FROM agentflow WHERE id = $1 AND workspace_id = $2;

-- name: ListAgentflows :many
SELECT * FROM agentflow
WHERE workspace_id = $1 AND status != 'archived'
ORDER BY created_at DESC;

-- name: ListAllAgentflows :many
SELECT * FROM agentflow
WHERE workspace_id = $1
ORDER BY created_at DESC;

-- name: UpdateAgentflow :one
UPDATE agentflow SET
    title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    agent_id = COALESCE(sqlc.narg('agent_id'), agent_id),
    status = COALESCE(sqlc.narg('status'), status),
    concurrency_policy = COALESCE(sqlc.narg('concurrency_policy'), concurrency_policy),
    variables = COALESCE(sqlc.narg('variables'), variables),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ArchiveAgentflow :one
UPDATE agentflow SET status = 'archived', updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CreateAgentflowTrigger :one
INSERT INTO agentflow_trigger (
    agentflow_id, kind, enabled,
    cron_expression, timezone, next_run_at,
    public_id, secret_hash, signing_mode
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetAgentflowTrigger :one
SELECT * FROM agentflow_trigger WHERE id = $1;

-- name: ListAgentflowTriggers :many
SELECT * FROM agentflow_trigger
WHERE agentflow_id = $1
ORDER BY created_at ASC;

-- name: UpdateAgentflowTrigger :one
UPDATE agentflow_trigger SET
    enabled = COALESCE(sqlc.narg('enabled'), enabled),
    cron_expression = COALESCE(sqlc.narg('cron_expression'), cron_expression),
    timezone = COALESCE(sqlc.narg('timezone'), timezone),
    next_run_at = COALESCE(sqlc.narg('next_run_at'), next_run_at)
WHERE id = $1
RETURNING *;

-- name: DeleteAgentflowTrigger :exec
DELETE FROM agentflow_trigger WHERE id = $1;

-- name: ClaimDueScheduleTriggers :many
-- Atomically claims all schedule triggers that are due.
-- Uses CAS (compare-and-swap) on next_run_at to prevent double-firing
-- across multiple server instances.
UPDATE agentflow_trigger t SET
    next_run_at = NULL, -- will be recalculated by caller
    last_fired_at = now()
FROM agentflow a
WHERE t.agentflow_id = a.id
  AND t.kind = 'schedule'
  AND t.enabled = true
  AND a.status = 'active'
  AND t.next_run_at IS NOT NULL
  AND t.next_run_at <= now()
RETURNING t.*, a.workspace_id, a.agent_id, a.title AS agentflow_title, a.description AS agentflow_description, a.concurrency_policy;

-- name: SetTriggerNextRunAt :exec
UPDATE agentflow_trigger SET next_run_at = $2
WHERE id = $1;

-- name: CreateAgentflowRun :one
INSERT INTO agentflow_run (
    agentflow_id, trigger_id, source_kind, status, payload, idempotency_key
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetAgentflowRun :one
SELECT * FROM agentflow_run WHERE id = $1;

-- name: ListAgentflowRuns :many
SELECT * FROM agentflow_run
WHERE agentflow_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: UpdateAgentflowRunStatus :one
UPDATE agentflow_run SET
    status = $2,
    started_at = CASE WHEN $2 = 'executing' THEN now() ELSE started_at END,
    completed_at = CASE WHEN $2 IN ('completed', 'failed', 'skipped', 'coalesced') THEN now() ELSE completed_at END
WHERE id = $1
RETURNING *;

-- name: CompleteAgentflowRun :one
UPDATE agentflow_run SET
    status = $2,
    agent_output = $3,
    linked_issue_id = sqlc.narg('linked_issue_id'),
    completed_at = now()
WHERE id = $1
RETURNING *;

-- name: HasActiveAgentflowRun :one
-- Check if agentflow has a run currently executing (for concurrency policy).
SELECT count(*) > 0 AS has_active FROM agentflow_run
WHERE agentflow_id = $1 AND status IN ('received', 'executing');

-- name: CreateAgentflowTask :one
-- Creates a task in the queue for an agentflow run (issue_id is NULL).
INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, agentflow_run_id)
VALUES ($1, $2, NULL, 'queued', $3, $4)
RETURNING *;

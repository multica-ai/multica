-- name: ListCerebroWorkflowHookPolicies :many
SELECT * FROM cerebro_workflow_hook_policy
WHERE workspace_id = $1
ORDER BY updated_at DESC;

-- name: GetCerebroWorkflowHookPolicy :one
SELECT * FROM cerebro_workflow_hook_policy
WHERE id = $1 AND workspace_id = $2;

-- name: CreateCerebroWorkflowHookPolicy :one
INSERT INTO cerebro_workflow_hook_policy (
    family_id, workspace_id, name, description, contract_rule, contract_satisfy, policy_version, mode, fail_mode,
    condition_mode, event_types, conditions, created_by_id, created_by_type
) VALUES ($1, $2, $3, $4, $5, $6, $7, 'dry_run', $8, COALESCE(NULLIF($9, ''), 'all'), $10, $11, $12, $13)
RETURNING *;

-- name: CreateCerebroWorkflowHookPolicyVersion :one
INSERT INTO cerebro_workflow_hook_policy (
    family_id, workspace_id, name, description, contract_rule, contract_satisfy, policy_version, mode, fail_mode,
    condition_mode, event_types, conditions, created_by_id, created_by_type
)
SELECT p.family_id, p.workspace_id, $3, $4, $5, $6, p.policy_version + 1, 'dry_run', $7,
       COALESCE(NULLIF($8, ''), 'all'), $9, $10, $11, $12
FROM cerebro_workflow_hook_policy p
WHERE p.id = $1 AND p.workspace_id = $2
RETURNING *;

-- name: DeleteCerebroWorkflowHookPolicy :exec
DELETE FROM cerebro_workflow_hook_policy
WHERE id = $1 AND workspace_id = $2 AND mode <> 'managed';

-- name: CreateCerebroWorkflowHookBinding :one
INSERT INTO cerebro_workflow_hook_binding (policy_id, scope_kind, scope_id, priority)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListCerebroWorkflowHookBindings :many
SELECT * FROM cerebro_workflow_hook_binding
WHERE policy_id = $1
ORDER BY priority DESC, created_at ASC;

-- name: CreateCerebroWorkflowHookHandler :one
INSERT INTO cerebro_workflow_hook_handler (
    policy_id, position, decision, requirement, modifications, actions
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListCerebroWorkflowHookHandlers :many
SELECT * FROM cerebro_workflow_hook_handler
WHERE policy_id = $1
ORDER BY position ASC;

-- name: CountCerebroWorkflowHookRuns :one
SELECT COUNT(*) FROM cerebro_workflow_hook_run
WHERE policy_id = $1;

-- name: PublishCerebroWorkflowHookPolicy :one
UPDATE cerebro_workflow_hook_policy p
SET mode = 'enforce', published_at = now(), published_by_id = $3, updated_at = now()
WHERE p.id = $1 AND p.workspace_id = $2 AND p.mode = 'dry_run'
  AND p.baseline_at IS NOT NULL
  AND EXISTS (SELECT 1 FROM cerebro_workflow_hook_run r WHERE r.policy_id = p.id)
RETURNING *;

-- name: MarkCerebroWorkflowHookBaselineFresh :exec
UPDATE cerebro_workflow_hook_policy
SET baseline_at = now(), updated_at = now()
WHERE id = $1 AND workspace_id = $2;

-- name: CreateCerebroWorkflowHookRun :one
INSERT INTO cerebro_workflow_hook_run (
    workspace_id, policy_id, policy_version, event_id, event_type, source_scope,
    input_event, matched_conditions, decision, would_decision, fail_mode,
    remediation, latency_ms, timed_out, idempotency_key
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
ON CONFLICT (workspace_id, idempotency_key) DO UPDATE
SET idempotency_key = EXCLUDED.idempotency_key
RETURNING *;

-- name: ListCerebroWorkflowHookRuns :many
SELECT * FROM cerebro_workflow_hook_run
WHERE workspace_id = $1 AND (sqlc.narg('policy_id')::uuid IS NULL OR policy_id = sqlc.narg('policy_id'))
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CreateCerebroWorkflowHookActionRun :one
INSERT INTO cerebro_workflow_hook_action_run (
    hook_run_id, handler_id, action_index, action_type, action_config, status,
    result, error, started_at, finished_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

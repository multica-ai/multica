-- name: ListCerebroWorkflowsForTrigger :many
-- Listener hot path: every domain event runs this query, so we filter on the
-- indexed (workspace_id, trigger_type) WHERE enabled = TRUE index. project_id
-- match is left to in-memory filtering because workflows scoped to a specific
-- project are still bound to the workspace.
SELECT id, workspace_id, project_id, name, enabled,
       trigger_type, trigger_config, conditions,
       action_type, action_config,
       created_by_id, created_by_type,
       created_at, updated_at,
       editor_mode, editor_layout,
       inbound_webhook_token, inbound_signing_secret, outbound_webhook_secret
FROM cerebro_workflow
WHERE workspace_id = $1
  AND trigger_type = $2
  AND enabled = TRUE;

-- name: ListCerebroWorkflows :many
SELECT id, workspace_id, project_id, name, enabled,
       trigger_type, trigger_config, conditions,
       action_type, action_config,
       created_by_id, created_by_type,
       created_at, updated_at,
       editor_mode, editor_layout,
       inbound_webhook_token, inbound_signing_secret, outbound_webhook_secret
FROM cerebro_workflow
WHERE workspace_id = $1
ORDER BY created_at DESC;

-- name: GetCerebroWorkflow :one
SELECT id, workspace_id, project_id, name, enabled,
       trigger_type, trigger_config, conditions,
       action_type, action_config,
       created_by_id, created_by_type,
       created_at, updated_at,
       editor_mode, editor_layout,
       inbound_webhook_token, inbound_signing_secret, outbound_webhook_secret
FROM cerebro_workflow
WHERE id = $1;

-- name: CreateCerebroWorkflow :one
INSERT INTO cerebro_workflow (
    workspace_id, project_id, name, enabled,
    trigger_type, trigger_config, conditions,
    action_type, action_config,
    created_by_id, created_by_type,
    editor_mode, editor_layout
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING id, workspace_id, project_id, name, enabled,
          trigger_type, trigger_config, conditions,
          action_type, action_config,
          created_by_id, created_by_type,
          created_at, updated_at,
          editor_mode, editor_layout,
          inbound_webhook_token, inbound_signing_secret, outbound_webhook_secret;

-- name: UpdateCerebroWorkflow :one
-- Phase-3 note (JEH-1108): inbound_webhook_token / inbound_signing_secret /
-- outbound_webhook_secret are deliberately NOT set here — they round-trip
-- through PR 2's dedicated regenerate-token endpoint. The RETURNING clause
-- includes them so callers see the existing values after an unrelated
-- update without a follow-up SELECT.
UPDATE cerebro_workflow
SET name = $2,
    enabled = $3,
    project_id = $4,
    trigger_type = $5,
    trigger_config = $6,
    conditions = $7,
    action_type = $8,
    action_config = $9,
    editor_mode = $10,
    editor_layout = $11,
    updated_at = now()
WHERE id = $1
RETURNING id, workspace_id, project_id, name, enabled,
          trigger_type, trigger_config, conditions,
          action_type, action_config,
          created_by_id, created_by_type,
          created_at, updated_at,
          editor_mode, editor_layout,
          inbound_webhook_token, inbound_signing_secret, outbound_webhook_secret;

-- name: SetCerebroWorkflowEnabled :exec
UPDATE cerebro_workflow
SET enabled = $2, updated_at = now()
WHERE id = $1;

-- name: DeleteCerebroWorkflow :exec
DELETE FROM cerebro_workflow WHERE id = $1;

-- name: InsertCerebroWorkflowIdempotencyKey :execrows
-- ON CONFLICT DO NOTHING returns 0 rows when the key already exists; the
-- service treats 0 as "skip, already handled" and !=0 as "I claimed it".
INSERT INTO cerebro_workflow_idempotency_key (key, workflow_id, run_id)
VALUES ($1, $2, $3)
ON CONFLICT (workflow_id, key) DO NOTHING;

-- name: CreateCerebroWorkflowRun :one
INSERT INTO cerebro_workflow_run (
    workflow_id, workspace_id, trigger_event, target_issue_id,
    status, attempt, started_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, workflow_id, workspace_id, trigger_event, target_issue_id,
          task_id, status, attempt, error,
          started_at, finished_at, next_retry_at, created_at;

-- name: MarkCerebroWorkflowRunSuccess :exec
UPDATE cerebro_workflow_run
SET status = 'success',
    task_id = $2,
    finished_at = now(),
    error = NULL,
    next_retry_at = NULL
WHERE id = $1;

-- name: MarkCerebroWorkflowRunFailed :exec
UPDATE cerebro_workflow_run
SET status = 'failed',
    error = $2,
    next_retry_at = $3,
    finished_at = now()
WHERE id = $1;

-- name: MarkCerebroWorkflowRunEscalated :exec
UPDATE cerebro_workflow_run
SET status = 'escalated',
    finished_at = now(),
    next_retry_at = NULL
WHERE id = $1;

-- name: BumpCerebroWorkflowRunAttempt :exec
UPDATE cerebro_workflow_run
SET attempt = $2,
    status = 'running',
    started_at = now()
WHERE id = $1;

-- name: ListPendingCerebroWorkflowRunRetries :many
SELECT id, workflow_id, workspace_id, trigger_event, target_issue_id,
       task_id, status, attempt, error,
       started_at, finished_at, next_retry_at, created_at
FROM cerebro_workflow_run
WHERE status = 'failed'
  AND next_retry_at IS NOT NULL
  AND next_retry_at <= $1
ORDER BY next_retry_at ASC
LIMIT $2;

-- name: ListCerebroWorkflowRuns :many
SELECT id, workflow_id, workspace_id, trigger_event, target_issue_id,
       task_id, status, attempt, error,
       started_at, finished_at, next_retry_at, created_at
FROM cerebro_workflow_run
WHERE workflow_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListWorkspaceCerebroWorkflowRuns :many
SELECT id, workflow_id, workspace_id, trigger_event, target_issue_id,
       task_id, status, attempt, error,
       started_at, finished_at, next_retry_at, created_at
FROM cerebro_workflow_run
WHERE workspace_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: UpsertCerebroIssueDueTime :one
INSERT INTO cerebro_issue_due_time (issue_id, due_at, set_by_id, set_by_type)
VALUES ($1, $2, $3, $4)
ON CONFLICT (issue_id) DO UPDATE
SET due_at = EXCLUDED.due_at,
    set_by_id = EXCLUDED.set_by_id,
    set_by_type = EXCLUDED.set_by_type,
    updated_at = now()
RETURNING issue_id, due_at, set_by_id, set_by_type, created_at, updated_at;

-- name: GetCerebroIssueDueTime :one
SELECT issue_id, due_at, set_by_id, set_by_type, created_at, updated_at
FROM cerebro_issue_due_time
WHERE issue_id = $1;

-- name: DeleteCerebroIssueDueTime :exec
DELETE FROM cerebro_issue_due_time WHERE issue_id = $1;

-- name: CountNonDoneChildrenLocked :one
-- Phase-3 helper for the all_children_done trigger (JEH-1108). Counts the
-- still-open siblings of a freshly-closed child issue while taking a
-- FOR UPDATE lock on the matching rows, so two concurrent
-- "last child → done" transitions serialize and the listener never fires
-- the trigger twice for the same all-done state.
--
-- The MAX(updated_at) over the parent's *done* children is what the EventID
-- hashes against — deterministic per all-done state, so re-opening a child
-- and re-closing it produces a new event_id and the trigger fires again.
WITH locked_children AS (
    SELECT status, updated_at
    FROM issue
    WHERE parent_issue_id = $1
      AND workspace_id = $2
    FOR UPDATE
)
SELECT
    COUNT(*) FILTER (WHERE status NOT IN ('done', 'cancelled'))::bigint AS open_count,
    (MAX(updated_at) FILTER (WHERE status IN ('done', 'cancelled')))::timestamptz AS last_done_at
FROM locked_children;

-- name: GetCerebroWorkflowByInboundToken :one
-- Phase-3 (JEH-1108, PR 2). Webhook ingress lookup. The partial-unique index
-- on (workspace_id, inbound_webhook_token) covers this — we filter on the
-- non-null token column directly, so the index lookup is O(1).
SELECT id, workspace_id, project_id, name, enabled,
       trigger_type, trigger_config, conditions,
       action_type, action_config,
       created_by_id, created_by_type,
       created_at, updated_at,
       editor_mode, editor_layout,
       inbound_webhook_token, inbound_signing_secret, outbound_webhook_secret
FROM cerebro_workflow
WHERE inbound_webhook_token = $1
LIMIT 1;

-- name: UpdateCerebroWorkflowInboundToken :exec
-- Phase-3 (JEH-1108, PR 2). Token regeneration is the only path that writes
-- inbound_webhook_token — the regular Update query deliberately omits the
-- column so a generic save can't clobber it.
UPDATE cerebro_workflow
SET inbound_webhook_token = $2,
    updated_at = now()
WHERE id = $1;

-- name: UpdateCerebroWorkflowInboundSigningSecret :exec
UPDATE cerebro_workflow
SET inbound_signing_secret = $2,
    updated_at = now()
WHERE id = $1;

-- name: UpdateCerebroWorkflowOutboundSecret :exec
UPDATE cerebro_workflow
SET outbound_webhook_secret = $2,
    updated_at = now()
WHERE id = $1;

-- name: ListEnabledCronWorkflowsAcrossWorkspaces :many
-- Phase-3 (JEH-1108, PR 2). Cron sweeper feed — multi-workspace, since the
-- sweeper runs once per process, not per workspace. Trigger_config carries
-- the schedule expression which the sweeper parses in Go.
SELECT id, workspace_id, project_id, name, enabled,
       trigger_type, trigger_config, conditions,
       action_type, action_config,
       created_by_id, created_by_type,
       created_at, updated_at,
       editor_mode, editor_layout,
       inbound_webhook_token, inbound_signing_secret, outbound_webhook_secret
FROM cerebro_workflow
WHERE trigger_type = 'cron'
  AND enabled = TRUE;

-- name: GetCerebroWorkflowCronState :one
SELECT workflow_id, last_fired_at, next_fire_at
FROM cerebro_workflow_cron_state
WHERE workflow_id = $1;

-- name: UpsertCerebroWorkflowCronState :exec
-- Sweeper writes one row per workflow on each fire. last_fired_at / next_fire_at
-- are nullable so the "first sweep" insert can leave next_fire_at NULL and
-- still satisfy the schema.
INSERT INTO cerebro_workflow_cron_state (workflow_id, last_fired_at, next_fire_at)
VALUES ($1, $2, $3)
ON CONFLICT (workflow_id) DO UPDATE
SET last_fired_at = EXCLUDED.last_fired_at,
    next_fire_at  = EXCLUDED.next_fire_at;

-- name: ListDueIssuesForSweeper :many
-- Joins upstream issue.due_date with the optional per-issue clock override.
-- The sweeper publishes due_date_reached / due_time_reached events for any
-- issue whose effective due time is in (after, before] and that has at least
-- one enabled workflow listening for it (filter applied in the service).
SELECT i.id AS issue_id,
       i.workspace_id,
       i.project_id,
       i.status,
       COALESCE(d.due_at, i.due_date) AS effective_due_at,
       d.due_at IS NOT NULL AS has_explicit_time
FROM issue i
LEFT JOIN cerebro_issue_due_time d ON d.issue_id = i.id
WHERE COALESCE(d.due_at, i.due_date) IS NOT NULL
  AND COALESCE(d.due_at, i.due_date) > $1
  AND COALESCE(d.due_at, i.due_date) <= $2
ORDER BY COALESCE(d.due_at, i.due_date) ASC
LIMIT $3;

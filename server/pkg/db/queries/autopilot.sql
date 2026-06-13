-- =====================
-- Autopilot CRUD
-- =====================

-- name: ListAutopilots :many
SELECT * FROM autopilot
WHERE workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY created_at DESC;

-- name: GetAutopilot :one
SELECT * FROM autopilot
WHERE id = $1;

-- name: GetAutopilotInWorkspace :one
SELECT * FROM autopilot
WHERE id = $1 AND workspace_id = $2;

-- name: CreateAutopilot :one
INSERT INTO autopilot (
    workspace_id, title, description, assignee_type, assignee_id,
    status, execution_mode, issue_title_template, project_id,
    created_by_type, created_by_id
) VALUES (
    $1, $2, sqlc.narg('description'), $3, $4,
    $5, $6, sqlc.narg('issue_title_template'), sqlc.narg('project_id'),
    $7, $8
) RETURNING *;

-- name: UpdateAutopilot :one
UPDATE autopilot SET
    title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    assignee_type = COALESCE(sqlc.narg('assignee_type'), assignee_type),
    assignee_id = COALESCE(sqlc.narg('assignee_id')::uuid, assignee_id),
    status = COALESCE(sqlc.narg('status'), status),
    execution_mode = COALESCE(sqlc.narg('execution_mode'), execution_mode),
    issue_title_template = sqlc.narg('issue_title_template'),
    project_id = sqlc.narg('project_id'),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteAutopilot :exec
DELETE FROM autopilot WHERE id = $1;

-- name: UpdateAutopilotLastRunAt :exec
UPDATE autopilot SET last_run_at = now(), updated_at = now()
WHERE id = $1;

-- =====================
-- Autopilot Trigger CRUD
-- =====================

-- name: ListAutopilotTriggers :many
SELECT * FROM autopilot_trigger
WHERE autopilot_id = $1
ORDER BY created_at ASC;

-- name: GetAutopilotTrigger :one
SELECT * FROM autopilot_trigger
WHERE id = $1;

-- name: CreateAutopilotTrigger :one
INSERT INTO autopilot_trigger (
    autopilot_id, kind, enabled, cron_expression, timezone,
    next_run_at, webhook_token, label, provider, event_filters
) VALUES (
    $1, $2, $3, sqlc.narg('cron_expression'), sqlc.narg('timezone'),
    sqlc.narg('next_run_at'), sqlc.narg('webhook_token'), sqlc.narg('label'),
    COALESCE(sqlc.narg('provider')::text, 'generic'),
    sqlc.narg('event_filters')
) RETURNING *;

-- name: UpdateAutopilotTrigger :one
UPDATE autopilot_trigger SET
    enabled = COALESCE(sqlc.narg('enabled')::boolean, enabled),
    cron_expression = COALESCE(sqlc.narg('cron_expression'), cron_expression),
    timezone = COALESCE(sqlc.narg('timezone'), timezone),
    next_run_at = sqlc.narg('next_run_at'),
    label = COALESCE(sqlc.narg('label'), label),
    event_filters = COALESCE(sqlc.narg('event_filters'), event_filters),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteAutopilotTrigger :exec
DELETE FROM autopilot_trigger WHERE id = $1;

-- name: AdvanceTriggerNextRun :exec
UPDATE autopilot_trigger
SET next_run_at = sqlc.narg('next_run_at'),
    last_fired_at = now(),
    updated_at = now()
WHERE id = $1;

-- name: GetWebhookTriggerByToken :one
-- Look up a webhook trigger by its public bearer token. Joined to autopilot
-- so the webhook handler can derive the workspace from the trigger's parent
-- without trusting any request header. The handler still re-loads the
-- Autopilot via GetAutopilot and cross-checks WorkspaceID matches the row's
-- autopilot_workspace_id.
SELECT t.*, a.workspace_id AS autopilot_workspace_id
FROM autopilot_trigger t
JOIN autopilot a ON a.id = t.autopilot_id
WHERE t.kind = 'webhook'
  AND t.webhook_token = $1;

-- name: TouchAutopilotTriggerFiredAt :exec
-- Bumps last_fired_at after a webhook fires, regardless of whether the
-- dispatch succeeded, was admission-skipped, or even if Autopilot status
-- transitioned to paused/disabled at exactly the wrong moment. Disabled /
-- paused early-return paths in the handler never call this.
UPDATE autopilot_trigger
SET last_fired_at = now(),
    updated_at = now()
WHERE id = $1;

-- name: RotateAutopilotTriggerWebhookToken :one
-- Rotates the bearer token for a webhook trigger. Restricted to kind='webhook'
-- so an accidental call against a schedule/api trigger is a no-op (returns no
-- rows) rather than corrupting unrelated state.
UPDATE autopilot_trigger
SET webhook_token = $2,
    updated_at = now()
WHERE id = $1
  AND kind = 'webhook'
RETURNING *;

-- name: SetAutopilotTriggerWebhookToken :one
-- Sets the webhook token at creation time. CreateAutopilotTrigger inserts the
-- row first (using its full 8-arg signature), then this query attaches the
-- token. Splitting the create + token-set keeps the existing CreateAutopilotTrigger
-- query usable by the schedule path without forcing every caller to think
-- about webhook_token.
UPDATE autopilot_trigger
SET webhook_token = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetAutopilotTriggerSigningSecret :one
-- Writes the signing secret for a webhook trigger. Kept as a dedicated query
-- (not a field on UpdateAutopilotTrigger) so the request body for the
-- write-only endpoint only ever carries the secret value, with no risk of an
-- accidental log line leaking it alongside other fields. Restricted to
-- webhook triggers to avoid corrupting unrelated state.
UPDATE autopilot_trigger
SET signing_secret = sqlc.narg('signing_secret'),
    updated_at = now()
WHERE id = $1
  AND kind = 'webhook'
RETURNING *;

-- =====================
-- Autopilot Run Management
-- =====================

-- name: CreateAutopilotRun :one
-- squad_id is an attribution hook: set to the assignee squad when the
-- parent autopilot has assignee_type='squad', NULL otherwise. The executing
-- agent_id on agent_task_queue still records who actually ran the work
-- (the squad leader); squad_id lets reports group by squad without a join.
INSERT INTO autopilot_run (
    autopilot_id, trigger_id, source, status, trigger_payload, squad_id
) VALUES (
    $1, sqlc.narg('trigger_id'), $2, $3, sqlc.narg('trigger_payload'),
    sqlc.narg('squad_id')
) RETURNING *;

-- name: GetAutopilotRun :one
SELECT * FROM autopilot_run
WHERE id = $1;

-- name: ListAutopilotRuns :many
SELECT * FROM autopilot_run
WHERE autopilot_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: UpdateAutopilotRunIssueCreated :one
UPDATE autopilot_run
SET status = 'issue_created', issue_id = $2
WHERE id = $1
RETURNING *;

-- name: UpdateAutopilotRunRunning :one
UPDATE autopilot_run
SET status = 'running', task_id = $2
WHERE id = $1
RETURNING *;

-- name: UpdateAutopilotRunCompleted :one
UPDATE autopilot_run
SET status = 'completed', completed_at = now(), result = sqlc.narg('result')
WHERE id = $1
RETURNING *;

-- name: UpdateAutopilotRunFailed :one
UPDATE autopilot_run
SET status = 'failed', completed_at = now(), failure_reason = $2
WHERE id = $1
RETURNING *;

-- name: UpdateAutopilotRunSkipped :one
-- Marks an autopilot_run as skipped without enqueueing any task. Used by the
-- pre-flight admission check when the assignee agent's runtime is offline:
-- creating an issue / task in that state would just pile a doomed job onto
-- agent_task_queue (the canonical "持续给离线 local agent 入队" symptom from
-- MUL-1899). Recording the skip + reason gives the UI / failure monitor / ops
-- a paper trail without polluting the failure ratio.
UPDATE autopilot_run
SET status = 'skipped', completed_at = now(), failure_reason = $2
WHERE id = $1
RETURNING *;

-- name: UpdateAutopilotRunSkippedWithResult :one
UPDATE autopilot_run
SET status = 'skipped',
    completed_at = now(),
    failure_reason = $2,
    result = sqlc.narg('result')
WHERE id = $1
RETURNING *;

-- =====================
-- Scheduler Queries
-- =====================

-- name: ClaimDueScheduleTriggers :many
-- Atomically claim all due schedule triggers to prevent concurrent execution.
-- Joins the autopilot table to ensure only active autopilots are fired.
UPDATE autopilot_trigger t
SET next_run_at = NULL
FROM autopilot a
WHERE t.autopilot_id = a.id
  AND t.kind = 'schedule'
  AND t.enabled = true
  AND t.next_run_at IS NOT NULL
  AND t.next_run_at <= now()
  AND a.status = 'active'
RETURNING t.*, a.workspace_id AS autopilot_workspace_id;

-- =====================
-- Task Queue (run_only mode)
-- =====================

-- name: CreateAutopilotTask :one
INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, autopilot_run_id, trigger_summary)
VALUES ($1, $2, NULL, 'queued', $3, $4, sqlc.narg(trigger_summary))
RETURNING *;

-- =====================
-- Run lookup by linked entities
-- =====================

-- name: GetAutopilotRunByIssue :one
SELECT * FROM autopilot_run
WHERE issue_id = $1 AND status IN ('issue_created', 'running')
LIMIT 1;

-- name: FailAutopilotRunsByIssue :exec
-- Fails active autopilot runs linked to a given issue.
-- Must be called BEFORE issue deletion (ON DELETE SET NULL clears issue_id).
UPDATE autopilot_run
SET status = 'failed', completed_at = now(), failure_reason = 'linked issue was deleted'
WHERE issue_id = $1
  AND status IN ('issue_created', 'running');

-- =====================
-- Scheduler Recovery
-- =====================

-- name: RecoverLostTriggers :many
-- Finds schedule triggers that were claimed (next_run_at = NULL) but never
-- advanced — typically due to a scheduler crash. Returns them so the scheduler
-- can recompute next_run_at.
SELECT t.*, a.workspace_id AS autopilot_workspace_id
FROM autopilot_trigger t
JOIN autopilot a ON t.autopilot_id = a.id
WHERE t.kind = 'schedule'
  AND t.enabled = true
  AND t.next_run_at IS NULL
  AND t.cron_expression IS NOT NULL
  AND a.status = 'active';

-- =====================
-- Failure-rate auto-pause
-- =====================

-- name: SelectAutopilotsExceedingFailureThreshold :many
-- Find active autopilots whose recent run failure rate exceeds the threshold.
-- Counts only "real" terminal runs (completed | failed). 'skipped' is
-- excluded from BOTH numerator and denominator: an admission-skipped run
-- (e.g. assignee runtime offline at dispatch time, MUL-1899) is neither a
-- success nor a failure, so it must not dilute the failure ratio (which
-- would let a 100%-failing autopilot mask itself behind a wall of skips)
-- nor inflate it. issue_created/running are still excluded so in-flight
-- work isn't penalised.
-- Used by the failure monitor to auto-pause sustained-failure autopilots
-- (the canonical example from MUL-1336 was an autopilot scheduled every 5 min
-- that 100% failed for days, burning ~1.5k useless tasks per week).
WITH stats AS (
    SELECT autopilot_id,
           count(*) FILTER (WHERE status IN ('completed', 'failed')) AS total,
           count(*) FILTER (WHERE status = 'failed') AS failed
    FROM autopilot_run
    WHERE created_at >= sqlc.arg('since')::timestamptz
    GROUP BY autopilot_id
)
SELECT a.id, a.workspace_id, a.title, a.assignee_id,
       a.created_by_type, a.created_by_id,
       s.total::bigint  AS total_runs,
       s.failed::bigint AS failed_runs
FROM autopilot a
JOIN stats s ON s.autopilot_id = a.id
WHERE a.status = 'active'
  AND s.total >= sqlc.arg('min_runs')::bigint
  AND s.failed::float8 / NULLIF(s.total, 0)::float8 >= sqlc.arg('fail_ratio_threshold')::float8
ORDER BY s.failed DESC, a.id ASC;

-- name: SystemPauseAutopilot :one
-- Atomically pauses an autopilot only if it is currently active. Returns no
-- rows when the autopilot was already paused/archived (or another worker
-- raced first), letting the caller treat that as a benign no-op rather than
-- an error.
UPDATE autopilot
SET status = 'paused', updated_at = now()
WHERE id = $1 AND status = 'active'
RETURNING *;

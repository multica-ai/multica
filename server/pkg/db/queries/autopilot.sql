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
    workspace_id, title, description, assignee_id,
    status, execution_mode, issue_title_template,
    created_by_type, created_by_id
) VALUES (
    $1, $2, sqlc.narg('description'), $3,
    $4, $5, sqlc.narg('issue_title_template'),
    $6, $7
) RETURNING *;

-- name: UpdateAutopilot :one
UPDATE autopilot SET
    title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    assignee_id = COALESCE(sqlc.narg('assignee_id')::uuid, assignee_id),
    status = COALESCE(sqlc.narg('status'), status),
    execution_mode = COALESCE(sqlc.narg('execution_mode'), execution_mode),
    issue_title_template = sqlc.narg('issue_title_template'),
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
    next_run_at, webhook_token, label
) VALUES (
    $1, $2, $3, sqlc.narg('cron_expression'), sqlc.narg('timezone'),
    sqlc.narg('next_run_at'), sqlc.narg('webhook_token'), sqlc.narg('label')
) RETURNING *;

-- name: UpdateAutopilotTrigger :one
UPDATE autopilot_trigger SET
    enabled = COALESCE(sqlc.narg('enabled')::boolean, enabled),
    cron_expression = COALESCE(sqlc.narg('cron_expression'), cron_expression),
    timezone = COALESCE(sqlc.narg('timezone'), timezone),
    next_run_at = sqlc.narg('next_run_at'),
    label = COALESCE(sqlc.narg('label'), label),
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

-- =====================
-- Autopilot Run Management
-- =====================

-- name: CreateAutopilotRun :one
INSERT INTO autopilot_run (
    autopilot_id, trigger_id, source, status, trigger_payload
) VALUES (
    $1, sqlc.narg('trigger_id'), $2, $3, sqlc.narg('trigger_payload')
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
-- Counts only terminal runs (completed | failed | skipped); pending,
-- issue_created and running are excluded so in-flight work isn't penalised.
-- Used by the failure monitor to auto-pause sustained-failure autopilots
-- (the canonical example from MUL-1336 was an autopilot scheduled every 5 min
-- that 100% failed for days, burning ~1.5k useless tasks per week).
WITH stats AS (
    SELECT autopilot_id,
           count(*) FILTER (WHERE status IN ('completed', 'failed', 'skipped')) AS total,
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

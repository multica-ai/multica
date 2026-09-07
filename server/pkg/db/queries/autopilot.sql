-- =====================
-- Autopilot CRUD
-- =====================

-- name: ListAutopilots :many
-- List rows carry three derived columns the list UI needs (trigger badges,
-- next run, last-run outcome) so the page never has to N+1 into the detail
-- endpoint. trigger_kinds/next_run_at only consider ENABLED triggers — the
-- columns answer "how does this fire today", not "what is configured".
-- last_run_status is COALESCEd to '' (never ran) because sqlc cannot infer
-- nullability through a scalar subquery; the handler maps '' back to omitted.
SELECT
  sqlc.embed(a),
  (
    SELECT array_agg(DISTINCT t.kind ORDER BY t.kind)
    FROM autopilot_trigger t
    WHERE t.autopilot_id = a.id AND t.enabled
  )::text[] AS trigger_kinds,
  (
    SELECT min(t.next_run_at)
    FROM autopilot_trigger t
    WHERE t.autopilot_id = a.id AND t.enabled AND t.kind = 'schedule'
  )::timestamptz AS next_run_at,
  COALESCE((
    SELECT r.status
    FROM autopilot_run r
    WHERE r.autopilot_id = a.id
    ORDER BY r.triggered_at DESC
    LIMIT 1
  ), '')::text AS last_run_status
FROM autopilot a
WHERE a.workspace_id = $1
  AND (
    (sqlc.narg('status')::text IS NULL AND a.status <> 'archived')
    OR a.status = sqlc.narg('status')
  )
ORDER BY a.created_at DESC;

-- name: GetAutopilot :one
SELECT * FROM autopilot
WHERE id = $1;

-- name: GetAutopilotInWorkspace :one
SELECT * FROM autopilot
WHERE id = $1 AND workspace_id = $2;

-- name: LockAutopilotForUpdate :one
-- UpdateAutopilot is a patch assembled from a pre-transaction snapshot. Lock
-- and compare that row before applying the patch so a concurrent retarget or
-- Runtime teardown cannot be overwritten by stale assignee/status fields.
SELECT * FROM autopilot
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

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
    pause_reason = CASE
      WHEN sqlc.narg('status')::text IS NOT NULL THEN NULL
      ELSE pause_reason
    END,
    execution_mode = COALESCE(sqlc.narg('execution_mode'), execution_mode),
    issue_title_template = sqlc.narg('issue_title_template'),
    project_id = sqlc.narg('project_id'),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ArchiveAutopilot :exec
UPDATE autopilot
SET status = 'archived', pause_reason = NULL, updated_at = now()
WHERE id = $1;

-- name: PauseAutopilotsByUnboundAgents :many
-- A runtime delete is a persistent admission failure, not a per-tick event.
-- Pause direct-agent automations and squad automations whose leader was
-- unbound, preserving the full configuration for an explicit resume after
-- rebind. Restrict to active so repeated teardown/retry is idempotent.
UPDATE autopilot a
SET status = 'paused',
    pause_reason = 'agent_runtime_required',
    updated_at = now()
WHERE a.status = 'active'
  AND (
    (a.assignee_type = 'agent' AND a.assignee_id = ANY(@agent_ids::uuid[]))
    OR (
      a.assignee_type = 'squad'
      AND EXISTS (
        SELECT 1
        FROM squad s
        WHERE s.id = a.assignee_id
          AND s.leader_id = ANY(@agent_ids::uuid[])
      )
    )
  )
RETURNING a.*;

-- name: PauseAutopilotsByUnrunnableSquad :many
-- Rotating a squad to an already-unbound leader has the same persistent
-- admission failure as Runtime teardown. Pause only automations assigned to
-- this squad; direct automations targeting that Agent are unrelated.
UPDATE autopilot
SET status = 'paused',
    pause_reason = 'agent_runtime_required',
    updated_at = now()
WHERE status = 'active'
  AND assignee_type = 'squad'
  AND assignee_id = @squad_id
RETURNING *;

-- name: UpdateAutopilotLastRunAt :exec
UPDATE autopilot SET last_run_at = now(), updated_at = now()
WHERE id = $1;

-- =====================
-- Autopilot Rule Version (rule_owner attribution, MUL-4302 §3.4)
-- =====================

-- name: CreateAutopilotRuleVersion :one
-- Append one immutable rule-version snapshot on a substantive publish (create /
-- enable / resume / target / execution-mode change). published_by_* is the acting
-- member (or 'system' with NULL id for the failure monitor); config_summary is the
-- effective config at publish time. Dispatch reads the latest row for the autopilot
-- as the run's rule_owner accountable human.
INSERT INTO autopilot_rule_version (
    autopilot_id, workspace_id, published_by_type, published_by_id, config_summary
)
VALUES (
    @autopilot_id, @workspace_id, @published_by_type, sqlc.narg(published_by_id),
    COALESCE(sqlc.narg(config_summary), '{}'::jsonb)
)
RETURNING *;

-- name: GetActiveAutopilotRuleVersion :one
-- The active version is the newest published row for the autopilot. Scoped by
-- workspace_id per the workspace query rule; autopilot_id is globally unique so the
-- workspace filter is a guard, not the selector.
SELECT * FROM autopilot_rule_version
WHERE workspace_id = $1 AND autopilot_id = $2
ORDER BY created_at DESC
LIMIT 1;

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

-- name: GetAutopilotTriggerForAutopilot :one
-- Trigger lookup BOUND to both the autopilot it must belong to AND that
-- autopilot's workspace. Since MUL-6951 the trigger row decides which human a run
-- acts as, so an unbound `WHERE id = $1` would let a trigger id from another
-- autopilot select the principal. The workspace join closes the other half: the
-- caller's membership check proves the resolved human belongs to the workspace it
-- passed, not that the AUTOPILOT does, and a member of two workspaces would
-- satisfy the former while the trigger came from the other tenant. Callers
-- resolving an authorization principal must use this, not GetAutopilotTrigger.
SELECT t.* FROM autopilot_trigger t
JOIN autopilot a ON a.id = t.autopilot_id
WHERE t.id = $1 AND t.autopilot_id = $2 AND a.workspace_id = $3;

-- name: CreateAutopilotTrigger :one
INSERT INTO autopilot_trigger (
    autopilot_id, kind, enabled, cron_expression, timezone,
    next_run_at, webhook_token, label, provider, event_filters,
    published_by_type, published_by_id,
    created_by_type, created_by_id
) VALUES (
    $1, $2, $3, sqlc.narg('cron_expression'), sqlc.narg('timezone'),
    sqlc.narg('next_run_at'), sqlc.narg('webhook_token'), sqlc.narg('label'),
    COALESCE(sqlc.narg('provider')::text, 'generic'),
    sqlc.narg('event_filters'),
    sqlc.narg('published_by_type'), sqlc.narg('published_by_id'),
    sqlc.narg('created_by_type'), sqlc.narg('created_by_id')
) RETURNING *;

-- name: SetAutopilotTriggerPublisher :exec
-- Re-stamp a single trigger's responsible publisher after a substantive edit of
-- THAT trigger (cron / filter / enabled / webhook security), recording who is now
-- responsible for its config (MUL-4302).
--
-- Since MUL-6951 this changes NOTHING about the runs it fires: they act as, and
-- are accountable to, the trigger's immutable created_by. An edit must not be able
-- to re-authorize the automation as the editor (Bohan's ruling), so this statement
-- deliberately does not touch created_by, and published_by is now a config-audit
-- column only.
UPDATE autopilot_trigger
SET published_by_type = $2, published_by_id = $3, updated_at = now()
WHERE id = $1;

-- name: SetAutopilotTriggerPublishersByAutopilot :exec
-- Re-stamp ALL of an autopilot's triggers' responsible publisher after a substantive
-- AUTOPILOT-level edit (target / instructions / assignee / execution-mode / enable).
-- Such a change governs every trigger's future runs, so responsibility transfers to
-- the editing member for all of them; a per-trigger edit uses the single-trigger
-- variant so it never reassigns another trigger (MUL-4302).
UPDATE autopilot_trigger
SET published_by_type = $2, published_by_id = $3, updated_at = now()
WHERE autopilot_id = $1;

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
--
-- planned_at carries the canonical UTC fire time for scheduled triggers
-- (source='schedule'); it stays NULL for manual / webhook / api sources
-- which have no canonical occurrence. Combined with the partial unique
-- index uq_autopilot_run_trigger_planned, this gives dispatch-layer
-- idempotency: a stale-steal retry at the same plan_time cannot create
-- a second run for the same (trigger_id, planned_at) pair (MUL-3551).
INSERT INTO autopilot_run (
    autopilot_id, trigger_id, source, status, trigger_payload, squad_id, planned_at,
    webhook_delivery_id, quota_reservation_id, reason_code, id
) VALUES (
    $1, sqlc.narg('trigger_id'), $2, $3, sqlc.narg('trigger_payload'),
    sqlc.narg('squad_id'), sqlc.narg('planned_at'),
    sqlc.narg('webhook_delivery_id'), sqlc.narg('quota_reservation_id'),
    sqlc.narg('reason_code'), COALESCE(sqlc.narg('id')::uuid, gen_random_uuid())
) RETURNING *;

-- name: GetAutopilotRunByTriggerAndPlanned :one
-- Idempotent lookup used by DispatchAutopilotForPlan to detect a
-- crash-during-dispatch retry: if a row already exists for this
-- (trigger_id, planned_at), the caller reuses it instead of creating a
-- duplicate. The partial unique index covers the same key, so a race
-- between "look up then insert" still resolves to a single row — this
-- query is just the fast path that lets us skip the INSERT when we
-- can see the prior row clearly. Returns no rows for the (much more
-- common) first-time dispatch.
SELECT * FROM autopilot_run
WHERE trigger_id = $1
  AND planned_at = $2
LIMIT 1;

-- name: GetAutopilotRunByWebhookDelivery :one
SELECT * FROM autopilot_run
WHERE webhook_delivery_id = $1
LIMIT 1;

-- name: GetAutopilotRunByQuotaReservation :one
SELECT * FROM autopilot_run
WHERE quota_reservation_id = $1
LIMIT 1;

-- name: RecoverPartialAutopilotRun :one
-- Recovers a partial-state autopilot_run from a crashed first attempt
-- (the runner wrote the run row but died before creating the downstream
-- issue/task) so that a subsequent DispatchAutopilotForPlan call can
-- create a fresh run at the same (trigger_id, planned_at).
--
-- Setting planned_at = NULL clears the partial-unique slot held by
-- uq_autopilot_run_trigger_planned, letting the new INSERT proceed.
-- The row stays in autopilot_run as a FAILED record (with a recovery
-- reason) so ops still see the abandoned attempt in the run history —
-- it is not silently deleted.
WITH updated_run AS (
    UPDATE autopilot_run AS ar
    SET status = 'failed',
        completed_at = now(),
        failure_reason = 'recovered partial dispatch (crashed before downstream creation)',
        reason_code = 'internal_error',
        planned_at = NULL
    WHERE ar.id = $1
      AND (
          ar.status = 'pending'
          OR (ar.status = 'issue_created' AND ar.issue_id IS NULL)
          OR (ar.status = 'running' AND ar.task_id IS NULL)
      )
      AND NOT EXISTS (
          SELECT 1
          FROM agent_task_queue task
          WHERE task.autopilot_run_id = ar.id
      )
    RETURNING ar.quota_reservation_id
), locked_reservation AS MATERIALIZED (
    SELECT qr.*
    FROM autopilot_quota_reservation qr
    JOIN updated_run ar ON ar.quota_reservation_id = qr.id
    WHERE qr.state = 'reserved'
    FOR UPDATE
), released_reservation AS (
    UPDATE autopilot_quota_reservation AS qr
    SET state = 'released', finalized_at = now()
    FROM locked_reservation AS locked
    WHERE qr.id = locked.id
      AND EXISTS (
          SELECT 1 FROM autopilot_quota_period p
          WHERE p.workspace_id = locked.workspace_id
            AND p.period_start = locked.period_start
            AND p.period_end = locked.period_end
      )
    RETURNING locked.workspace_id, locked.period_start, locked.period_end
), settled_period AS (
    UPDATE autopilot_quota_period AS p
    SET reserved_count = reserved_count - 1,
        updated_at = now()
    FROM released_reservation AS released
    WHERE p.workspace_id = released.workspace_id
      AND p.period_start = released.period_start
      AND p.period_end = released.period_end
    RETURNING p.workspace_id
)
SELECT count(*)::bigint FROM updated_run;

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
-- Quota safety: only use for a run known to have no reservation. Normal
-- terminal paths must call UpdateAutopilotRunTerminalWithQuota instead.
UPDATE autopilot_run
SET status = 'completed', completed_at = now(), result = sqlc.narg('result')
WHERE id = $1
RETURNING *;

-- name: UpdateAutopilotRunFailed :one
-- Quota safety: only use for a run known to have no reservation. Normal
-- terminal paths must call UpdateAutopilotRunTerminalWithQuota instead.
UPDATE autopilot_run
SET status = 'failed', completed_at = now(), failure_reason = $2,
    reason_code = sqlc.narg('reason_code')
WHERE id = $1
RETURNING *;

-- name: UpdateAutopilotRunSkipped :one
-- Quota safety: this is only for pre-admission skipped rows, which never have
-- reservations. Post-admission skips use UpdateAutopilotRunTerminalWithQuota.
-- Marks an autopilot_run as skipped without enqueueing any task. Used by the
-- pre-flight admission check when the assignee agent's runtime is offline:
-- creating an issue / task in that state would just pile a doomed job onto
-- agent_task_queue (the canonical "持续给离线 local agent 入队" symptom from
-- MUL-1899). Recording the skip + reason gives the UI / failure monitor / ops
-- a paper trail without polluting the failure ratio.
UPDATE autopilot_run
SET status = 'skipped', completed_at = now(), failure_reason = $2,
    reason_code = sqlc.narg('reason_code')
WHERE id = $1
RETURNING *;

-- name: UpdateAutopilotRunTerminalWithQuota :one
-- Finalizes a run and its still-reserved quota slot in one statement. Runs
-- created while the entitlement gate is off have a NULL reservation ID, so
-- the quota CTEs become empty without an extra BEGIN/COMMIT round trip.
-- Consumed slots are deliberately immutable: create_issue is chargeable once
-- the issue exists, even if that issue is later blocked, cancelled, or deleted.
-- The CTE sequence is: update the run, lock a still-reserved slot, finalize
-- that slot exactly once, then move one unit from reserved_count to either
-- used_count (consume) or nowhere (release). used_count never decreases.
WITH updated_run AS (
    UPDATE autopilot_run AS ar
    SET status = @terminal_status::text,
        completed_at = now(),
        result = CASE
            WHEN @terminal_status::text = 'completed' THEN sqlc.narg('result')::jsonb
            ELSE ar.result
        END,
        failure_reason = CASE
            WHEN @terminal_status::text IN ('failed', 'skipped') THEN sqlc.narg('failure_reason')::text
            ELSE ar.failure_reason
        END,
        reason_code = CASE
            WHEN @terminal_status::text IN ('failed', 'skipped') THEN sqlc.narg('reason_code')::text
            ELSE ar.reason_code
        END
    WHERE ar.id = @run_id
    RETURNING ar.*
), locked_reservation AS MATERIALIZED (
    SELECT qr.*
    FROM autopilot_quota_reservation qr
    JOIN updated_run ar ON ar.quota_reservation_id = qr.id
    WHERE qr.state = 'reserved'
    FOR UPDATE
), finalized_reservation AS (
    UPDATE autopilot_quota_reservation AS qr
    SET state = CASE WHEN @consume::boolean THEN 'consumed' ELSE 'released' END,
        finalized_at = now()
    FROM locked_reservation AS locked
    WHERE qr.id = locked.id
      AND EXISTS (
          SELECT 1 FROM autopilot_quota_period p
          WHERE p.workspace_id = locked.workspace_id
            AND p.period_start = locked.period_start
            AND p.period_end = locked.period_end
      )
    RETURNING locked.workspace_id, locked.period_start, locked.period_end
), settled_period AS (
    UPDATE autopilot_quota_period AS p
    SET reserved_count = reserved_count - 1,
        used_count = used_count + CASE WHEN @consume::boolean THEN 1 ELSE 0 END,
        updated_at = now()
    FROM finalized_reservation AS finalized
    WHERE p.workspace_id = finalized.workspace_id
      AND p.period_start = finalized.period_start
      AND p.period_end = finalized.period_end
    RETURNING p.workspace_id
)
SELECT * FROM updated_run;

-- name: UpdateAutopilotRunSkippedWithResult :one
-- Quota safety: legacy no-reservation helper. New terminal paths must call
-- UpdateAutopilotRunTerminalWithQuota instead.
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

-- name: ListSchedulableAutopilotTriggers :many
-- Lists every schedule trigger the autopilot_schedule_dispatch JobSpec
-- should consider this tick. Returns just the columns the scheduler's
-- scope provider + PlansForScope hook need; the full trigger row is
-- re-loaded by the handler so a trigger update between scope-list and
-- handler-run sees the latest enabled / cron values.
--
-- last_fired_at is read so the planner hook can anchor cold-start
-- enumeration on the most recent successful fire (set by either the
-- legacy goroutine before the new scheduler took over, or the new
-- scheduler's own post-dispatch advance — AdvanceTriggerNextRun, falling
-- back to TouchAutopilotTriggerFiredAt on a cron parse error). Without it,
-- a trigger that was created days ago and fired by the legacy code
-- looks like a brand-new trigger to the new scheduler on first tick
-- and the half-open `(created_at, now]` enumeration replays the most
-- recent already-fired occurrence — exactly the post-deploy
-- spurious-fire reported on MUL-3551 dev.
--
-- Filters out webhook / api triggers, disabled triggers, paused/archived
-- autopilots, and any trigger missing its cron expression. ORDER BY id
-- keeps the per-tick scope list stable across replicas.
SELECT t.id, t.autopilot_id, t.cron_expression, t.timezone, t.created_at, t.last_fired_at
FROM autopilot_trigger t
JOIN autopilot a ON a.id = t.autopilot_id
WHERE t.kind = 'schedule'
  AND t.enabled = TRUE
  AND a.status = 'active'
  AND t.cron_expression IS NOT NULL
  AND t.cron_expression <> ''
ORDER BY t.id;

-- =====================
-- Task Queue (run_only mode)
-- =====================

-- name: CreateAutopilotTask :one
-- Fenced against workspace teardown: lock_task_owner_rows (migration 284)
-- locks the owners' workspace rows in the writer's own transaction and returns
-- false once they are gone, so this statement writes no row instead of stranding
-- a task in a workspace that has just been deleted (MUL-5999).
-- run_only autopilot dispatch. Attribution depends on the trigger:
--   * schedule / webhook / api: no human authorized the run, so originator_user_id
--     stays NULL and accountable_user_id is the rule_owner (the publisher of the
--     autopilot's active rule version), with rule_version_id recording the snapshot
--     (MUL-4302 §3.4) — the accountable-diverges-from-originator case.
--   * manual: a member clicked "run now", a direct human action, so originator and
--     accountable are BOTH that member (originator_source='direct_human'); no rule
--     version is involved (MUL-4302 §4).
-- When no version/publisher resolves on the non-manual path, the caller passes NULL
-- accountable + originator_source='unattributed' so the row is still not a
-- NULL-source bypass (MUL-4302 §2).
INSERT INTO agent_task_queue (
    agent_id, runtime_id, issue_id, status, priority, autopilot_run_id, trigger_summary,
    originator_user_id, accountable_user_id, rule_version_id,
    originator_source, trigger_evidence_kind, trigger_evidence_ref_id,
    id
)
SELECT
    $1, $2, NULL, 'queued', $3, $4, sqlc.narg(trigger_summary),
    sqlc.narg(originator_user_id),
    sqlc.narg(accountable_user_id),
    sqlc.narg(rule_version_id),
    sqlc.narg(originator_source),
    sqlc.narg(trigger_evidence_kind),
    sqlc.narg(trigger_evidence_ref_id),
    COALESCE(sqlc.narg('id')::uuid, gen_random_uuid())
WHERE lock_task_owner_rows($1, NULL, $2)
RETURNING *;

-- name: GetAutopilotTaskByRun :one
-- Repairs the narrow run_only crash window where the task INSERT committed
-- but the following autopilot_run.task_id update did not.
SELECT * FROM agent_task_queue
WHERE autopilot_run_id = $1
ORDER BY created_at
LIMIT 1;

-- =====================
-- Run lookup by linked entities
-- =====================

-- name: GetAutopilotRunByIssue :one
SELECT * FROM autopilot_run
WHERE issue_id = $1 AND status IN ('issue_created', 'running')
LIMIT 1;

-- name: FailAutopilotRunsByIssue :many
-- Fails active autopilot runs linked to a given issue.
-- Must be called BEFORE issue deletion (ON DELETE SET NULL clears issue_id).
-- Only still-reserved run_only slots are released. A create_issue slot was
-- consumed when the issue was created and remains counted after deletion.
WITH updated_runs AS (
    UPDATE autopilot_run
    SET status = 'failed', completed_at = now(), failure_reason = 'linked issue was deleted'
    WHERE issue_id = $1
      AND status IN ('issue_created', 'running')
    RETURNING *
), locked_reservations AS MATERIALIZED (
    SELECT qr.*
    FROM autopilot_quota_reservation qr
    JOIN updated_runs ar ON ar.quota_reservation_id = qr.id
    WHERE qr.state = 'reserved'
    FOR UPDATE
), released_reservations AS (
    UPDATE autopilot_quota_reservation AS qr
    SET state = 'released', finalized_at = now()
    FROM locked_reservations AS locked
    WHERE qr.id = locked.id
      AND EXISTS (
          SELECT 1 FROM autopilot_quota_period p
          WHERE p.workspace_id = locked.workspace_id
            AND p.period_start = locked.period_start
            AND p.period_end = locked.period_end
      )
    RETURNING locked.workspace_id, locked.period_start, locked.period_end
), released_by_period AS (
    SELECT workspace_id, period_start, period_end, count(*)::bigint AS released_count
    FROM released_reservations
    GROUP BY workspace_id, period_start, period_end
), settled_periods AS (
    UPDATE autopilot_quota_period AS p
    SET reserved_count = reserved_count - released.released_count,
        updated_at = now()
    FROM released_by_period AS released
    WHERE p.workspace_id = released.workspace_id
      AND p.period_start = released.period_start
      AND p.period_end = released.period_end
    RETURNING p.workspace_id
)
SELECT * FROM updated_runs;

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
SET status = 'paused', pause_reason = NULL, updated_at = now()
WHERE id = $1 AND status = 'active'
RETURNING *;

-- =====================
-- Autopilot Subscribers
-- =====================

-- name: ListAutopilotSubscribers :many
-- Only current workspace members are effective subscribers. The membership
-- join makes legacy rows left behind by older member-removal code inert on
-- both the detail response and the dispatch path, so clients never round-trip
-- a hidden departed member into an otherwise valid update.
-- ORDER BY created_at keeps chip rendering stable across refreshes.
SELECT s.* FROM autopilot_subscriber AS s
JOIN autopilot AS a ON a.id = s.autopilot_id
JOIN member AS m
  ON m.workspace_id = a.workspace_id
 AND m.user_id = s.user_id
WHERE s.autopilot_id = $1
  AND s.user_type = 'member'
ORDER BY s.created_at ASC, s.user_id ASC;

-- name: ListAutopilotSubscribersForAutopilots :many
-- Batch form of ListAutopilotSubscribers for the list endpoint, which must not
-- issue one query per row. The autopilot_subscriber primary key leads with
-- autopilot_id, so ANY($1) is index-supported and no extra index is needed.
-- The member join and ordering are identical to the single-autopilot query on
-- purpose: list and detail have to agree on who counts as a subscriber, or the
-- two projections disagree again (MUL-6680).
SELECT s.* FROM autopilot_subscriber AS s
JOIN autopilot AS a ON a.id = s.autopilot_id
JOIN member AS m
  ON m.workspace_id = a.workspace_id
 AND m.user_id = s.user_id
WHERE s.autopilot_id = ANY($1::uuid[])
  AND s.user_type = 'member'
ORDER BY s.autopilot_id ASC, s.created_at ASC, s.user_id ASC;

-- name: AddAutopilotSubscriber :exec
INSERT INTO autopilot_subscriber (autopilot_id, user_type, user_id)
VALUES ($1, $2, $3)
ON CONFLICT (autopilot_id, user_type, user_id) DO NOTHING;

-- name: DeleteAutopilotSubscribersForAutopilot :exec
-- Paired with a re-insert loop to implement full-replace PATCH semantics.
DELETE FROM autopilot_subscriber
WHERE autopilot_id = $1;

-- name: DeleteAutopilotSubscribersByMember :exec
-- Autopilot subscribers carry no FK, so member removal must prune them in the
-- same application transaction. Scope through the autopilot's workspace: the
-- same user may remain subscribed to autopilots in another workspace.
DELETE FROM autopilot_subscriber AS s
USING autopilot AS a
WHERE s.autopilot_id = a.id
  AND a.workspace_id = sqlc.arg(workspace_id)
  AND s.user_type = 'member'
  AND s.user_id = sqlc.arg(user_id);

-- =====================
-- Autopilot Collaborators
-- =====================

-- name: ListAutopilotCollaborators :many
-- ORDER BY created_at keeps row rendering stable across refreshes.
SELECT * FROM autopilot_collaborator
WHERE autopilot_id = $1
ORDER BY created_at ASC, user_id ASC;

-- name: AddAutopilotCollaborator :one
-- Re-granting an existing collaborator is a no-op that refreshes granted_by,
-- so the call is idempotent from the API boundary.
INSERT INTO autopilot_collaborator (autopilot_id, user_type, user_id, granted_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (autopilot_id, user_type, user_id)
    DO UPDATE SET granted_by = EXCLUDED.granted_by
RETURNING *;

-- name: DeleteAutopilotCollaborator :exec
DELETE FROM autopilot_collaborator
WHERE autopilot_id = $1 AND user_type = $2 AND user_id = $3;

-- name: DeleteAutopilotCollaboratorsForAutopilot :exec
-- Application-layer cleanup run inside the autopilot delete transaction.
DELETE FROM autopilot_collaborator
WHERE autopilot_id = $1;

-- name: IsAutopilotCollaborator :one
SELECT EXISTS (
    SELECT 1 FROM autopilot_collaborator
    WHERE autopilot_id = $1 AND user_type = 'member' AND user_id = $2
) AS is_collaborator;

-- name: ListAutopilotIDsForCollaborator :many
-- Powers the per-row can_write flag on the list endpoint without an N+1.
SELECT autopilot_id FROM autopilot_collaborator
WHERE user_type = 'member' AND user_id = $1;

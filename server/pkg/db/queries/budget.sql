-- Budgets (spec 4.2). Attribution: a task belongs to a squad through
-- agent_task_queue.squad_id, else its agent's home squad; to a floor through
-- its squad; to an initiative through its backing issue or any issue of the
-- initiative's build project. Spend is accounted per task into budget_period.

-- name: ListWorkspaceBudgetStates :many
-- Every budget of the workspace with its current period's counters. The period
-- join matches the row whose window contains @now; a budget with no spend yet
-- has no row, and its counters read as zero.
SELECT b.id, b.workspace_id, b.scope_type, b.scope_id, b.period, b.amount_micros,
       b.soft_percent, b.mode, b.classes, b.status, b.created_by, b.created_at, b.updated_at,
       COALESCE(p.spent_micros, 0)::bigint AS spent_micros,
       COALESCE(p.extended_micros, 0)::bigint AS extended_micros,
       COALESCE(p.runs, 0)::integer AS runs,
       p.period_start, p.period_end
FROM budget b
LEFT JOIN budget_period p
  ON p.budget_id = b.id AND p.period_start <= @now::timestamptz AND p.period_end > @now::timestamptz
WHERE b.workspace_id = @workspace_id::uuid
ORDER BY b.scope_type, b.created_at, b.id;

-- name: GetBudgetForWorkspace :one
SELECT * FROM budget WHERE id = @id::uuid AND workspace_id = @workspace_id::uuid;

-- name: ListActiveBudgetsByWorkspaces :many
SELECT * FROM budget WHERE workspace_id = ANY(@workspace_ids::uuid[]) AND status = 'active';

-- name: ListActiveBudgetWorkspaces :many
SELECT DISTINCT workspace_id FROM budget WHERE status = 'active';

-- name: InsertBudget :one
INSERT INTO budget (id, workspace_id, scope_type, scope_id, period, amount_micros, soft_percent, mode, classes, status, created_by)
VALUES (@id::uuid, @workspace_id::uuid, @scope_type, sqlc.narg('scope_id')::uuid, @period, @amount_micros, @soft_percent, @mode, @classes::text[], @status, sqlc.narg('created_by')::uuid)
RETURNING *;

-- name: UpdateBudget :one
UPDATE budget
SET amount_micros = @amount_micros,
    soft_percent = @soft_percent,
    mode = @mode,
    classes = @classes::text[],
    status = @status,
    updated_at = now()
WHERE id = @id::uuid AND workspace_id = @workspace_id::uuid
RETURNING *;

-- name: EnsureBudgetPeriod :exec
INSERT INTO budget_period (budget_id, workspace_id, period_start, period_end, computed_at)
VALUES (@budget_id::uuid, @workspace_id::uuid, @period_start::timestamptz, @period_end::timestamptz, now())
ON CONFLICT (budget_id, period_start) DO NOTHING;

-- name: AddBudgetPeriodSpend :exec
INSERT INTO budget_period (budget_id, workspace_id, period_start, period_end, spent_micros, runs, computed_at)
VALUES (@budget_id::uuid, @workspace_id::uuid, @period_start::timestamptz, @period_end::timestamptz, @spent_micros::bigint, @runs::integer, now())
ON CONFLICT (budget_id, period_start) DO UPDATE
SET spent_micros = budget_period.spent_micros + EXCLUDED.spent_micros,
    runs = budget_period.runs + EXCLUDED.runs,
    computed_at = now();

-- name: SetBudgetPeriodState :exec
UPDATE budget_period SET state = @state, computed_at = now()
WHERE budget_id = @budget_id::uuid AND period_start = @period_start::timestamptz;

-- name: AddBudgetPeriodExtension :exec
UPDATE budget_period
SET extended_micros = extended_micros + @amount_micros::bigint, computed_at = now()
WHERE budget_id = @budget_id::uuid AND period_start = @period_start::timestamptz;

-- name: ListBudgetPeriods :many
SELECT * FROM budget_period
WHERE budget_id = @budget_id::uuid
ORDER BY period_start DESC
LIMIT @row_limit::integer;

-- name: ListTaskUsageTasksChangedSince :many
-- Tasks whose usage rows changed after the cursor, oldest change first. The
-- accounting job advances its cursor to the last change it processed.
SELECT task_id, MAX(updated_at)::timestamptz AS changed_at
FROM task_usage
WHERE updated_at > @since::timestamptz
GROUP BY task_id
ORDER BY MAX(updated_at)
LIMIT @row_limit::integer;

-- name: LockBudgetTaskForAccounting :one
-- The task row and its attribution, locked so two accounting passes over the
-- same task serialise.
SELECT t.id, a.workspace_id, t.created_at, t.budget_accounted_micros,
       COALESCE(t.squad_id, a.home_squad_id) AS squad_id,
       (SELECT s.floor_id FROM squad s WHERE s.id = COALESCE(t.squad_id, a.home_squad_id)) AS floor_id,
       (SELECT ini.id FROM initiative ini
         LEFT JOIN issue iss ON iss.id = t.issue_id
         WHERE ini.workspace_id = a.workspace_id
           AND (ini.issue_id = t.issue_id OR (iss.project_id IS NOT NULL AND ini.build_project_id = iss.project_id))
         ORDER BY ini.created_at, ini.id
         LIMIT 1) AS initiative_id
FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
WHERE t.id = @task_id::uuid
FOR UPDATE OF t;

-- name: SetBudgetTaskAccounted :exec
UPDATE agent_task_queue SET budget_accounted_micros = @budget_accounted_micros::bigint WHERE id = @task_id::uuid;

-- name: GetBudgetTaskScope :one
-- The attribution of one task, read without a lock (admission and reads).
SELECT a.workspace_id, t.created_at, t.budget_accounted_micros,
       COALESCE(t.squad_id, a.home_squad_id) AS squad_id,
       (SELECT s.floor_id FROM squad s WHERE s.id = COALESCE(t.squad_id, a.home_squad_id)) AS floor_id,
       (SELECT ini.id FROM initiative ini
         LEFT JOIN issue iss ON iss.id = t.issue_id
         WHERE ini.workspace_id = a.workspace_id
           AND (ini.issue_id = t.issue_id OR (iss.project_id IS NOT NULL AND ini.build_project_id = iss.project_id))
         ORDER BY ini.created_at, ini.id
         LIMIT 1) AS initiative_id
FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
WHERE t.id = @task_id::uuid;

-- name: ListBudgetInflightTasks :many
-- Tasks the workspace has in flight, with their attribution, except the task
-- being admitted. The reservation counts these per scope.
SELECT t.id,
       COALESCE(t.squad_id, a.home_squad_id) AS squad_id,
       (SELECT s.floor_id FROM squad s WHERE s.id = COALESCE(t.squad_id, a.home_squad_id)) AS floor_id,
       (SELECT ini.id FROM initiative ini
         LEFT JOIN issue iss ON iss.id = t.issue_id
         WHERE ini.workspace_id = a.workspace_id
           AND (ini.issue_id = t.issue_id OR (iss.project_id IS NOT NULL AND ini.build_project_id = iss.project_id))
         ORDER BY ini.created_at, ini.id
         LIMIT 1) AS initiative_id
FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
WHERE a.workspace_id = @workspace_id::uuid
  AND t.status IN ('dispatched', 'running', 'waiting_local_directory')
  AND t.id IS DISTINCT FROM sqlc.narg('exclude_task_id')::uuid;

-- name: ListBudgetFinishedTaskCosts :many
-- Completed tasks of the workspace since a date, with their accounted cost and
-- attribution. The trailing 90th percentile of a scope is taken over these.
SELECT t.budget_accounted_micros,
       COALESCE(t.squad_id, a.home_squad_id) AS squad_id,
       (SELECT s.floor_id FROM squad s WHERE s.id = COALESCE(t.squad_id, a.home_squad_id)) AS floor_id,
       (SELECT ini.id FROM initiative ini
         LEFT JOIN issue iss ON iss.id = t.issue_id
         WHERE ini.workspace_id = a.workspace_id
           AND (ini.issue_id = t.issue_id OR (iss.project_id IS NOT NULL AND ini.build_project_id = iss.project_id))
         ORDER BY ini.created_at, ini.id
         LIMIT 1) AS initiative_id
FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
WHERE a.workspace_id = @workspace_id::uuid
  AND t.status = 'completed'
  AND t.completed_at >= @since::timestamptz;

-- name: InsertBudgetHoldIfNone :execrows
-- One hold in force per task: a second refusal of the same task inserts nothing.
INSERT INTO budget_hold (id, workspace_id, budget_id, task_id, agent_id, squad_id, run_class)
VALUES (@id::uuid, @workspace_id::uuid, @budget_id::uuid, @task_id::uuid, sqlc.narg('agent_id')::uuid, sqlc.narg('squad_id')::uuid, @run_class)
ON CONFLICT (task_id) WHERE released_at IS NULL DO NOTHING;

-- name: HasOverriddenBudgetHold :one
-- A hold the boss released once with "release holds": the task may run once.
SELECT EXISTS (
  SELECT 1 FROM budget_hold WHERE task_id = @task_id::uuid AND release_reason = 'overridden'
) AS overridden;

-- name: ReleaseBudgetHoldForTask :execrows
UPDATE budget_hold
SET released_at = now(), release_reason = @release_reason
WHERE task_id = @task_id::uuid AND released_at IS NULL;

-- name: ReleaseBudgetHoldsForBudget :execrows
UPDATE budget_hold
SET released_at = now(), release_reason = @release_reason
WHERE budget_id = @budget_id::uuid AND released_at IS NULL;

-- name: ReleaseBudgetHoldsCreatedBefore :execrows
UPDATE budget_hold
SET released_at = now(), release_reason = @release_reason
WHERE budget_id = @budget_id::uuid AND released_at IS NULL AND created_at < @before::timestamptz;

-- name: ReleaseBudgetHoldsForEndedTasks :execrows
-- Holds whose task ended (cancelled, completed or failed) release as cancelled.
UPDATE budget_hold h
SET released_at = now(), release_reason = 'cancelled'
WHERE h.released_at IS NULL
  AND EXISTS (
    SELECT 1 FROM agent_task_queue t
    WHERE t.id = h.task_id AND t.status IN ('completed', 'failed', 'cancelled')
  );

-- name: ListBudgetHoldsInForce :many
SELECT h.id, h.workspace_id, h.budget_id, h.task_id, h.agent_id, h.squad_id, h.run_class,
       h.created_at, b.scope_type, b.scope_id, b.period, b.amount_micros, b.mode
FROM budget_hold h
JOIN budget b ON b.id = h.budget_id
WHERE h.workspace_id = @workspace_id::uuid AND h.released_at IS NULL
ORDER BY h.created_at DESC, h.id DESC
LIMIT @row_limit::integer;

-- name: ListBudgetIDsWithHoldsInForce :many
SELECT DISTINCT budget_id FROM budget_hold WHERE workspace_id = @workspace_id::uuid AND released_at IS NULL;

-- name: ListTaskUsageRowsForBudget :many
-- The usage rows of one task, read to price the task when it changed.
SELECT provider, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
       COALESCE(cost_usd_ticks, 0)::bigint AS cost_usd_ticks
FROM task_usage
WHERE task_id = @task_id::uuid;

-- name: GetBudgetPeriod :one
SELECT * FROM budget_period
WHERE budget_id = @budget_id::uuid AND period_start = @period_start::timestamptz;

-- name: LockBudgetWorkspace :exec
-- Serialises budget accounting and budget creation per workspace, so a new
-- budget's backfill and a concurrent accounting pass cannot both count a task.
SELECT pg_advisory_xact_lock(hashtextextended('budget:' || @workspace_id::text, 0));

-- name: ListBudgetTaskCostsSince :many
-- Every task of the workspace created since a time, with its accounted cost
-- and attribution. A new budget's backfill sums the ones in its scope.
SELECT t.id, t.created_at, t.budget_accounted_micros,
       COALESCE(t.squad_id, a.home_squad_id) AS squad_id,
       (SELECT s.floor_id FROM squad s WHERE s.id = COALESCE(t.squad_id, a.home_squad_id)) AS floor_id,
       (SELECT ini.id FROM initiative ini
         LEFT JOIN issue iss ON iss.id = t.issue_id
         WHERE ini.workspace_id = a.workspace_id
           AND (ini.issue_id = t.issue_id OR (iss.project_id IS NOT NULL AND ini.build_project_id = iss.project_id))
         ORDER BY ini.created_at, ini.id
         LIMIT 1) AS initiative_id
FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
WHERE a.workspace_id = @workspace_id::uuid
  AND t.created_at >= @since::timestamptz;

-- name: ListTaskIDsWithUsageSince :many
-- Tasks of the workspace created since a time that have a usage row; the
-- backfill brings each one up to date before it sums them.
SELECT DISTINCT u.task_id
FROM task_usage u
JOIN agent_task_queue t ON t.id = u.task_id
JOIN agent a ON a.id = t.agent_id
WHERE a.workspace_id = @workspace_id::uuid
  AND t.created_at >= @since::timestamptz;

-- name: GetBudgetSquadFloor :one
SELECT s.id, s.floor_id FROM squad s WHERE s.id = @squad_id::uuid;

-- name: ListQuietDeferredAutopilotRuns :many
-- Scheduled or routine runs that quiet hours deferred and that are waiting for
-- the window to close. The sweep dispatches them, oldest first.
SELECT * FROM autopilot_run
WHERE status = 'deferred' AND reason_code = 'quiet_hours'
ORDER BY created_at, id
LIMIT @row_limit::integer;

-- name: DeleteWorkspaceBudgetHolds :exec
-- Workspace teardown only: budgets and their holds go with the workspace.
DELETE FROM budget_hold WHERE workspace_id = @workspace_id::uuid;

-- name: DeleteWorkspaceBudgetPeriods :exec
DELETE FROM budget_period WHERE workspace_id = @workspace_id::uuid;

-- name: DeleteWorkspaceBudgets :exec
DELETE FROM budget WHERE workspace_id = @workspace_id::uuid;

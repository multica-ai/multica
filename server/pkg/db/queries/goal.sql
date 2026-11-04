-- Goals, key results and the planning cycle (spec 4.1). Every query filters by
-- workspace_id. A goal's key results are written with it and removed with it in
-- one transaction (no foreign keys).

-- name: CreateGoal :one
INSERT INTO goal (
    id, workspace_id, scope_type, scope_id, parent_goal_id, title, why,
    period_start, period_end, owner_type, owner_id, status,
    source_proposal_id, created_by_type, created_by_id
) VALUES (
    @id, @workspace_id, @scope_type, sqlc.narg('scope_id'), sqlc.narg('parent_goal_id'), @title, @why,
    @period_start, @period_end, @owner_type, @owner_id, @status,
    sqlc.narg('source_proposal_id'), @created_by_type, sqlc.narg('created_by_id')
)
RETURNING *;

-- name: GetGoalInWorkspace :one
SELECT * FROM goal WHERE id = $1 AND workspace_id = $2;

-- name: LockGoalInWorkspace :one
SELECT * FROM goal WHERE id = $1 AND workspace_id = $2 FOR UPDATE;

-- name: ListGoalsInWorkspace :many
-- The goal tree's filters are optional; NULL does not filter.
SELECT * FROM goal
WHERE workspace_id = @workspace_id
  AND (sqlc.narg('scope_type')::text IS NULL OR scope_type = sqlc.narg('scope_type')::text)
  AND (sqlc.narg('scope_id')::uuid IS NULL OR scope_id = sqlc.narg('scope_id')::uuid)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
ORDER BY period_end DESC, created_at DESC, id DESC;

-- name: ListGoalsByStatus :many
SELECT * FROM goal
WHERE workspace_id = @workspace_id AND status = ANY(@statuses::text[])
ORDER BY created_at, id;

-- name: UpdateGoal :one
UPDATE goal SET
    title = @title,
    why = @why,
    period_start = @period_start,
    period_end = @period_end,
    owner_type = @owner_type,
    owner_id = @owner_id,
    parent_goal_id = sqlc.narg('parent_goal_id'),
    updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id
RETURNING *;

-- name: SetGoalStatus :one
UPDATE goal SET status = @status, updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id
RETURNING *;

-- name: SetGoalDerivedConfidence :one
-- confidence_changed_at moves only when the derived value changes; the brief
-- reports goals whose confidence moved since its last run.
UPDATE goal SET
    confidence = @confidence,
    confidence_changed_at = CASE WHEN confidence IS DISTINCT FROM @confidence THEN now() ELSE confidence_changed_at END
WHERE id = @id AND workspace_id = @workspace_id
RETURNING *;

-- name: SetGoalConfidenceOverride :one
UPDATE goal SET
    confidence_override = sqlc.narg('confidence_override'),
    confidence_note = @confidence_note,
    updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id
RETURNING *;

-- name: DeleteDraftGoal :execrows
DELETE FROM goal WHERE id = $1 AND workspace_id = $2 AND status = 'draft';

-- name: DeleteWorkspaceGoals :exec
DELETE FROM goal WHERE workspace_id = $1;

-- name: CreateKeyResult :one
INSERT INTO key_result (
    id, workspace_id, goal_id, title, metric_key, scope_type, scope_id,
    start_value, target_value, weight
) VALUES (
    @id, @workspace_id, @goal_id, @title, @metric_key, @scope_type, sqlc.narg('scope_id'),
    @start_value, @target_value, @weight
)
RETURNING *;

-- name: ListKeyResultsForGoal :many
SELECT * FROM key_result WHERE workspace_id = $1 AND goal_id = $2 ORDER BY created_at, id;

-- name: DeleteGoalKeyResults :exec
DELETE FROM key_result WHERE workspace_id = $1 AND goal_id = $2;

-- name: DeleteWorkspaceKeyResults :exec
DELETE FROM key_result WHERE workspace_id = $1;

-- name: ListActiveKeyResultsForSquad :many
-- Active squad goals whose period holds today (a UTC date), for the leader's briefing.
SELECT kr.id AS key_result_id, kr.goal_id, g.title AS goal_title, kr.title, kr.metric_key,
       kr.scope_type, kr.scope_id, kr.start_value, kr.target_value, kr.weight,
       r.value AS current_value, r.observed_at AS observed_at
FROM key_result kr
JOIN goal g ON g.id = kr.goal_id AND g.workspace_id = kr.workspace_id
LEFT JOIN LATERAL (
    SELECT mr.value, mr.observed_at
    FROM metric_reading mr
    WHERE mr.workspace_id = kr.workspace_id
      AND mr.metric_key = kr.metric_key
      AND mr.scope_type = kr.scope_type
      AND mr.scope_id IS NOT DISTINCT FROM kr.scope_id
      AND mr.observed_at >= (g.period_start::timestamp AT TIME ZONE 'UTC')
      AND mr.observed_at < ((g.period_end + 1)::timestamp AT TIME ZONE 'UTC')
    ORDER BY mr.observed_at DESC, mr.created_at DESC, mr.id DESC
    LIMIT 1
) r ON true
WHERE kr.workspace_id = @workspace_id
  AND g.scope_type = 'squad'
  AND g.scope_id = @squad_id
  AND g.status = 'active'
  AND g.period_start <= sqlc.arg('today')::date
  AND g.period_end >= sqlc.arg('today')::date
ORDER BY g.created_at, g.id, kr.created_at, kr.id;

-- name: ListPlanningSquads :many
-- Live squads with a charter, the ones the planning cycle opens an issue for.
SELECT s.id AS squad_id, s.name, s.leader_id, s.workspace_id
FROM squad s
JOIN squad_charter c ON c.squad_id = s.id AND c.workspace_id = s.workspace_id
WHERE s.workspace_id = @workspace_id
  AND s.archived_at IS NULL
ORDER BY s.created_at, s.id;

-- name: InsertPlanningCycle :one
-- A second run for the same period and squad inserts nothing and returns no row.
INSERT INTO planning_cycle (id, workspace_id, period_start, squad_id, issue_id)
VALUES (@id, @workspace_id, @period_start, @squad_id, @issue_id)
ON CONFLICT (workspace_id, period_start, squad_id) DO NOTHING
RETURNING id;

-- name: DeleteWorkspacePlanningCycles :exec
DELETE FROM planning_cycle WHERE workspace_id = $1;

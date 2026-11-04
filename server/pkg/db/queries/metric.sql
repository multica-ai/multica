-- Metrics and readings (spec 4.1). Every query filters by workspace_id, because
-- there are no foreign keys to do it. Readings are append-only except the
-- system readings the nightly job rewrites for its own day and the retention
-- job's duplicate removal.

-- name: SeedBuiltinMetric :exec
-- Idempotent: a built-in metric already present keeps its row.
INSERT INTO metric (workspace_id, key, name, unit, source, direction, description)
VALUES (@workspace_id, @key, @name, @unit, 'builtin', @direction, @description)
ON CONFLICT (workspace_id, key) DO NOTHING;

-- name: GetMetricInWorkspace :one
SELECT * FROM metric WHERE workspace_id = $1 AND key = $2;

-- name: ListMetricsInWorkspace :many
SELECT * FROM metric WHERE workspace_id = $1 ORDER BY source, key;

-- name: EnsureManualMetric :one
-- The first report of a manual or webhook metric creates it; a later one returns
-- the existing row unchanged.
INSERT INTO metric (workspace_id, key, name, unit, source, direction, description)
VALUES (@workspace_id, @key, @name, @unit, @source, @direction, '')
ON CONFLICT (workspace_id, key) DO UPDATE SET key = EXCLUDED.key
RETURNING *;

-- name: DeleteWorkspaceMetricReadings :exec
DELETE FROM metric_reading WHERE workspace_id = $1;

-- name: DeleteWorkspaceMetrics :exec
DELETE FROM metric WHERE workspace_id = $1;

-- name: InsertMetricReading :one
INSERT INTO metric_reading (id, workspace_id, metric_key, scope_type, scope_id, value, observed_at, actor_type, actor_id, note)
VALUES (@id, @workspace_id, @metric_key, @scope_type, sqlc.narg('scope_id'), @value, @observed_at, @actor_type, sqlc.narg('actor_id'), @note)
RETURNING *;

-- name: DeleteSystemMetricReadingsAt :exec
-- The nightly job replaces its own reading for a day, so a rerun leaves one.
DELETE FROM metric_reading
WHERE workspace_id = @workspace_id
  AND metric_key = @metric_key
  AND scope_type = @scope_type
  AND scope_id IS NOT DISTINCT FROM sqlc.narg('scope_id')
  AND observed_at = @observed_at
  AND actor_type = 'system';

-- name: ListMetricReadingsForSeries :many
-- One series, newest first, in [since, until).
SELECT * FROM metric_reading
WHERE workspace_id = @workspace_id
  AND metric_key = @metric_key
  AND scope_type = @scope_type
  AND scope_id IS NOT DISTINCT FROM sqlc.narg('scope_id')
  AND observed_at >= @since
  AND observed_at < @until
ORDER BY observed_at DESC, created_at DESC, id DESC
LIMIT @row_limit;

-- name: ListKeyResultLatestReadings :many
-- Each key result of the workspace's goals in @statuses (or of one goal when
-- goal_id is set), with the latest reading of its metric at its scope inside
-- its goal's period. current_value is NULL when no reading falls in the period.
SELECT kr.id AS key_result_id, kr.goal_id, kr.title, kr.metric_key, kr.scope_type, kr.scope_id,
       kr.start_value, kr.target_value, kr.weight,
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
  AND g.status = ANY(@statuses::text[])
  AND (sqlc.narg('goal_id')::uuid IS NULL OR kr.goal_id = sqlc.narg('goal_id')::uuid)
ORDER BY g.created_at, g.id, kr.created_at, kr.id;

-- name: DeleteDuplicateMetricReadingsBefore :execrows
-- Retention (spec 4.9): for readings older than the cutoff, keep the newest
-- reading of each series per UTC day and delete the rest. Bounded by row_limit
-- so one run stays short; the next night continues.
DELETE FROM metric_reading mr
USING (
    SELECT ranked.id FROM (
        SELECT mr0.id, row_number() OVER (
            PARTITION BY mr0.workspace_id, mr0.metric_key, mr0.scope_type,
                         COALESCE(mr0.scope_id, '00000000-0000-0000-0000-000000000000'::uuid),
                         (mr0.observed_at AT TIME ZONE 'UTC')::date
            ORDER BY mr0.observed_at DESC, mr0.created_at DESC, mr0.id DESC
        ) AS rn
        FROM metric_reading mr0
        WHERE mr0.observed_at < @cutoff
    ) ranked
    WHERE ranked.rn > 1
    LIMIT @row_limit
) dup
WHERE mr.id = dup.id;

-- name: BuiltinRunStats :one
-- Finished runs of the workspace's agents in [since, until), and their
-- provider-reported cost (1e-10 USD). squad_ids NULL is the whole workspace.
SELECT
    count(*) FILTER (WHERE t.status = 'completed')::int AS completed,
    count(*) FILTER (WHERE t.status = 'failed')::int AS failed,
    COALESCE(SUM(u.cost_usd_ticks), 0)::bigint AS cost_usd_ticks
FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id AND a.workspace_id = @workspace_id
LEFT JOIN LATERAL (
    SELECT SUM(tu.cost_usd_ticks) AS cost_usd_ticks FROM task_usage tu WHERE tu.task_id = t.id
) u ON true
WHERE t.status IN ('completed', 'failed')
  AND t.completed_at >= @since
  AND t.completed_at < @until
  AND (sqlc.narg('squad_ids')::uuid[] IS NULL OR t.squad_id = ANY(sqlc.narg('squad_ids')::uuid[]));

-- name: BuiltinProposalStats :one
-- Proposals decided in [since, until). decision_seconds sums the time from
-- filing to decision.
SELECT
    count(*) FILTER (WHERE p.state = 'approved')::int AS approved,
    count(*) FILTER (WHERE p.state = 'rejected')::int AS rejected,
    COALESCE(SUM(EXTRACT(EPOCH FROM (p.decided_at - p.created_at))), 0)::float8 AS decision_seconds
FROM proposal p
WHERE p.workspace_id = @workspace_id
  AND p.state IN ('approved', 'rejected')
  AND p.decided_at >= @since
  AND p.decided_at < @until
  AND (sqlc.narg('squad_ids')::uuid[] IS NULL OR p.squad_id = ANY(sqlc.narg('squad_ids')::uuid[]));

-- name: BuiltinIssueDeliveryStats :one
-- Squad issues done in [since, until), and the days from creation to done summed
-- over them (delivery.cycle_time_days is the mean). The done time is done_at
-- (migration 880), or updated_at for an issue done before done_at existed. A
-- later edit does not move a day's count, because done_at is set once per done
-- transition.
WITH done AS (
    SELECT i.created_at, COALESCE(i.done_at, i.updated_at) AS done_time
    FROM issue i
    LEFT JOIN issue_status st ON st.workspace_id = i.workspace_id AND st.key = i.status
    WHERE i.workspace_id = @workspace_id
      AND i.assignee_type = 'squad'
      AND (sqlc.narg('squad_ids')::uuid[] IS NULL OR i.assignee_id = ANY(sqlc.narg('squad_ids')::uuid[]))
      AND (i.status = 'done' OR st.category = 'done')
      AND COALESCE(i.done_at, i.updated_at) >= @since
      AND COALESCE(i.done_at, i.updated_at) < @until
)
SELECT count(*)::int AS done,
       COALESCE(sum(EXTRACT(EPOCH FROM (done_time - created_at)) / 86400), 0)::float8 AS cycle_days_total
FROM done;

-- name: BuiltinIssuesReopened :one
-- Squad issues reopened out of done in [since, until) (migration 881 sets
-- last_reopened_at on each reopen). Rework is this count over the done count.
SELECT count(*)::int AS reopened
FROM issue i
WHERE i.workspace_id = @workspace_id
  AND i.assignee_type = 'squad'
  AND (sqlc.narg('squad_ids')::uuid[] IS NULL OR i.assignee_id = ANY(sqlc.narg('squad_ids')::uuid[]))
  AND i.last_reopened_at >= @since
  AND i.last_reopened_at < @until;

-- name: BuiltinKnowledgePublished :one
SELECT
    count(*) FILTER (WHERE kl.state = 'published' AND kl.published_at >= @since AND kl.published_at < @until)::int AS published_in_window,
    count(*) FILTER (WHERE kl.state = 'published')::int AS published_total
FROM knowledge_link kl
WHERE kl.workspace_id = @workspace_id;

-- name: BuiltinBriefUsefulness :one
SELECT
    count(*) FILTER (WHERE bf.rating = 'useful')::int AS useful,
    count(*)::int AS total
FROM brief_feedback bf
WHERE bf.workspace_id = @workspace_id
  AND bf.updated_at >= @since
  AND bf.updated_at < @until;

-- name: BuiltinKnowledgeBySquad :many
-- Published knowledge in live bases of a ready workspace, attributed to the home
-- squad of the agent that published it. Documents published by members, or by
-- agents with no home squad, belong to the company only. stale_total counts the
-- documents the digest calls stale (published before @published_before).
SELECT a.home_squad_id AS squad_id,
       count(*) FILTER (WHERE l.published_at >= @since AND l.published_at < @until)::int AS published_in_window,
       count(*)::int AS published_total,
       count(*) FILTER (WHERE l.published_at < @published_before)::int AS stale_total
FROM knowledge_link l
JOIN knowledge_base kb ON kb.id = l.knowledge_base_id AND kb.workspace_id = l.workspace_id
JOIN workspace_knowledge wk ON wk.workspace_id = l.workspace_id AND wk.state = 'ready'
JOIN agent a ON a.id = l.created_by_id AND a.workspace_id = l.workspace_id
WHERE l.workspace_id = @workspace_id
  AND l.created_by_type = 'agent'
  AND l.state = 'published'
  AND kb.archived_at IS NULL
  AND a.home_squad_id IS NOT NULL
GROUP BY a.home_squad_id;

-- name: BuiltinFloorBriefUsefulness :one
-- Ratings of the floor briefs of one floor, in [since, until). A floor brief has
-- scope_type 'floor' and its floor as scope_id (migration 840).
SELECT
    count(*) FILTER (WHERE bf.rating = 'useful')::int AS useful,
    count(*)::int AS total
FROM brief_feedback bf
JOIN brief b ON b.id = bf.brief_id AND b.workspace_id = bf.workspace_id
WHERE bf.workspace_id = @workspace_id
  AND b.scope_type = 'floor'
  AND b.scope_id = @floor_id
  AND bf.updated_at >= @since
  AND bf.updated_at < @until;

-- name: ListRetrievalHitRateRuns :many
-- Newest first. The retrieval reading is the latest eval run's hit rate.
SELECT id, hit_rate, ran_at FROM knowledge_eval_run
WHERE workspace_id = @workspace_id
ORDER BY ran_at DESC, id DESC
LIMIT @row_limit;

-- name: ListAllWorkspaceIDsForGovernance :many
SELECT id FROM workspace ORDER BY created_at ASC, id ASC;

-- name: ListLiveSquadScopes :many
SELECT id, floor_id FROM squad
WHERE workspace_id = @workspace_id AND archived_at IS NULL
ORDER BY id;

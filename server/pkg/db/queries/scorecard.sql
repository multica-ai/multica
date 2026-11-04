-- Scorecards (spec 4.6). The metric inputs below are counts and rows; the rates
-- and the status are derived in internal/service/scorecard_metrics.go, so the
-- arithmetic can be tested without a database.
--
-- A scope is one of: an agent (agent_id), a squad (squad_id, reached through
-- the task's squad or the agent's home squad), or the company (both NULL).

-- name: UpsertScorecard :one
INSERT INTO scorecard (
    id, workspace_id, subject_type, subject_id, period, period_start, period_end,
    metrics, sample_size, status, computed_at
) VALUES (
    @id, @workspace_id, @subject_type, @subject_id, @period, @period_start, @period_end,
    @metrics, @sample_size, @status, now()
)
ON CONFLICT (workspace_id, subject_type, subject_id, period, period_start) DO UPDATE SET
    period_end = EXCLUDED.period_end,
    metrics = EXCLUDED.metrics,
    sample_size = EXCLUDED.sample_size,
    status = EXCLUDED.status,
    computed_at = EXCLUDED.computed_at
RETURNING *;

-- name: GetScorecardForPeriod :one
SELECT * FROM scorecard
WHERE workspace_id = @workspace_id
  AND subject_type = @subject_type
  AND subject_id = @subject_id
  AND period = @period
  AND period_start = @period_start;

-- name: ListScorecards :many
-- Scorecards of a workspace, newest period first. subject_ids, when given,
-- narrows the list to those subjects; the handler sets it for callers who see
-- only some subjects.
SELECT * FROM scorecard
WHERE workspace_id = @workspace_id
  AND (sqlc.narg('subject_type')::text IS NULL OR subject_type = sqlc.narg('subject_type')::text)
  AND (sqlc.narg('subject_id')::uuid IS NULL OR subject_id = sqlc.narg('subject_id')::uuid)
  AND (sqlc.narg('subject_ids')::uuid[] IS NULL OR subject_id = ANY(sqlc.narg('subject_ids')::uuid[]))
  AND (sqlc.narg('period')::text IS NULL OR period = sqlc.narg('period')::text)
ORDER BY period_start DESC, subject_type, subject_id
LIMIT @row_limit;

-- name: GetLatestOKScorecard :one
-- The newest scorecard of a subject for a period that has ended by as_of, which
-- the probation check uses for its before and after figures.
SELECT * FROM scorecard
WHERE workspace_id = @workspace_id
  AND subject_type = @subject_type
  AND subject_id = @subject_id
  AND period = @period
  AND status = 'ok'
  AND period_end <= @as_of::date
ORDER BY period_end DESC
LIMIT 1;

-- name: ScorecardRunCounts :one
-- Terminal runs in the window. A failure caused by the runtime (offline, lost
-- in recovery, unusable, missing its profile, or access denied) or ended by a
-- person (manual) is not charged to the agent: it counts as uncharged.
SELECT
    COUNT(*) FILTER (WHERE t.status = 'completed')::bigint AS completed,
    COUNT(*) FILTER (WHERE t.status = 'failed' AND (t.failure_reason IS NULL OR t.failure_reason NOT IN
        ('runtime_offline', 'runtime_recovery', 'runtime_unusable', 'runtime_profile_missing',
         'runtime_access_denied', 'agent_runtime_required', 'manual')))::bigint AS charged_failed,
    COUNT(*) FILTER (WHERE t.status = 'failed' AND t.failure_reason IN
        ('runtime_offline', 'runtime_recovery', 'runtime_unusable', 'runtime_profile_missing',
         'runtime_access_denied', 'agent_runtime_required', 'manual'))::bigint AS uncharged_failed
FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
WHERE a.workspace_id = @workspace_id
  AND t.status IN ('completed', 'failed')
  AND t.completed_at >= @period_start::timestamptz
  AND t.completed_at < @period_end::timestamptz
  AND (sqlc.narg('agent_id')::uuid IS NULL OR t.agent_id = sqlc.narg('agent_id')::uuid)
  AND (sqlc.narg('squad_id')::uuid IS NULL OR COALESCE(t.squad_id, a.home_squad_id) = sqlc.narg('squad_id')::uuid);

-- name: ScorecardFailureCauses :many
-- Failed runs in the window by cause, so the scorecard shows what was charged
-- and what was not.
SELECT COALESCE(t.failure_reason, 'unknown')::text AS cause, COUNT(*)::bigint AS runs
FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
WHERE a.workspace_id = @workspace_id
  AND t.status = 'failed'
  AND t.completed_at >= @period_start::timestamptz
  AND t.completed_at < @period_end::timestamptz
  AND (sqlc.narg('agent_id')::uuid IS NULL OR t.agent_id = sqlc.narg('agent_id')::uuid)
  AND (sqlc.narg('squad_id')::uuid IS NULL OR COALESCE(t.squad_id, a.home_squad_id) = sqlc.narg('squad_id')::uuid)
GROUP BY 1
ORDER BY 1;

-- name: ScorecardDoneIssues :many
-- Issues in the scope that are done in the window. The done time is done_at
-- (migration 880), or updated_at for an issue done before done_at existed.
-- completed_runs counts the completed runs on the issue; reopen_count is how
-- often it was reopened (first-pass rate, spec 4.6). human_after is true when a
-- person commented after the issue was done: the human intervention the metric
-- counts. Edits are not recorded by the platform, so they are not counted.
SELECT
    i.id,
    i.created_at,
    COALESCE(i.done_at, i.updated_at) AS done_at,
    i.reopen_count,
    (SELECT COUNT(*) FROM agent_task_queue r WHERE r.issue_id = i.id AND r.status = 'completed')::bigint AS completed_runs,
    EXISTS (
        SELECT 1 FROM comment c
        WHERE c.issue_id = i.id
          AND c.author_type = 'member'
          AND c.created_at > COALESCE(i.done_at, i.updated_at)
    ) AS human_after
FROM issue i
LEFT JOIN issue_status st ON st.workspace_id = i.workspace_id AND st.key = i.status
WHERE i.workspace_id = @workspace_id
  AND (i.status = 'done' OR st.category = 'done')
  AND COALESCE(i.done_at, i.updated_at) >= @period_start::timestamptz
  AND COALESCE(i.done_at, i.updated_at) < @period_end::timestamptz
  AND (sqlc.narg('agent_id')::uuid IS NULL OR (i.assignee_type = 'agent' AND i.assignee_id = sqlc.narg('agent_id')::uuid))
  AND (sqlc.narg('squad_id')::uuid IS NULL OR (
        (i.assignee_type = 'squad' AND i.assignee_id = sqlc.narg('squad_id')::uuid)
        OR (i.assignee_type = 'agent' AND EXISTS (
            SELECT 1 FROM squad_member sm
            WHERE sm.squad_id = sqlc.narg('squad_id')::uuid
              AND sm.member_type = 'agent'
              AND sm.member_id = i.assignee_id))));

-- name: ScorecardRunCost :one
-- Provider-reported cost of the runs in the window, in 1e-10 USD, and the
-- tokens of runs the provider priced nowhere (those are not in the cost).
SELECT
    COALESCE(SUM(u.cost_usd_ticks), 0)::bigint AS priced_ticks,
    COALESCE(SUM(u.input_tokens + u.output_tokens) FILTER (WHERE u.cost_usd_ticks IS NULL), 0)::bigint AS unpriced_tokens
FROM task_usage u
JOIN agent_task_queue t ON t.id = u.task_id
JOIN agent a ON a.id = t.agent_id
WHERE a.workspace_id = @workspace_id
  AND t.status IN ('completed', 'failed')
  AND t.completed_at >= @period_start::timestamptz
  AND t.completed_at < @period_end::timestamptz
  AND (sqlc.narg('agent_id')::uuid IS NULL OR t.agent_id = sqlc.narg('agent_id')::uuid)
  AND (sqlc.narg('squad_id')::uuid IS NULL OR COALESCE(t.squad_id, a.home_squad_id) = sqlc.narg('squad_id')::uuid);

-- name: ScorecardProposalCounts :one
-- Proposals filed in the window by the scope and how they were decided. An
-- expired proposal is a non-acceptance (spec 2.4).
SELECT
    COUNT(*) FILTER (WHERE p.state = 'approved')::bigint AS approved,
    COUNT(*) FILTER (WHERE p.state IN ('approved', 'rejected', 'expired'))::bigint AS decided
FROM proposal p
WHERE p.workspace_id = @workspace_id
  AND p.created_at >= @period_start::timestamptz
  AND p.created_at < @period_end::timestamptz
  AND (sqlc.narg('agent_id')::uuid IS NULL OR (p.created_by_type = 'agent' AND p.created_by_id = sqlc.narg('agent_id')::uuid))
  AND (sqlc.narg('squad_id')::uuid IS NULL OR p.squad_id = sqlc.narg('squad_id')::uuid);

-- name: ScorecardKnowledgeCounts :one
-- Knowledge contribution: documents the scope published in the window, and
-- review verdicts the scope gave in the window.
SELECT
    COUNT(*) FILTER (WHERE k.state = 'published'
        AND k.published_at >= @period_start::timestamptz
        AND k.published_at < @period_end::timestamptz
        AND (sqlc.narg('agent_id')::uuid IS NULL OR k.created_by_id = sqlc.narg('agent_id')::uuid)
        AND (sqlc.narg('squad_id')::uuid IS NULL OR k.squad_id = sqlc.narg('squad_id')::uuid)
    )::bigint AS published,
    COUNT(*) FILTER (WHERE k.review_verdict IS NOT NULL
        AND k.reviewed_at >= @period_start::timestamptz
        AND k.reviewed_at < @period_end::timestamptz
        AND (sqlc.narg('agent_id')::uuid IS NULL OR k.review_by_id = sqlc.narg('agent_id')::uuid)
        AND (sqlc.narg('squad_id')::uuid IS NULL OR k.review_squad_id = sqlc.narg('squad_id')::uuid)
    )::bigint AS reviewed
FROM knowledge_link k
WHERE k.workspace_id = @workspace_id
  AND (sqlc.narg('agent_id')::uuid IS NOT NULL OR sqlc.narg('squad_id')::uuid IS NOT NULL
       OR (k.state = 'published' AND k.published_at >= @period_start::timestamptz AND k.published_at < @period_end::timestamptz)
       OR (k.reviewed_at >= @period_start::timestamptz AND k.reviewed_at < @period_end::timestamptz));

-- name: ScorecardBriefFeedback :one
-- The boss's ratings of the briefs a Secretary agent wrote in the window.
SELECT
    COUNT(*) FILTER (WHERE f.rating = 'useful')::bigint AS useful,
    COUNT(*)::bigint AS total
FROM brief_feedback f
JOIN brief b ON b.id = f.brief_id AND b.workspace_id = f.workspace_id
WHERE f.workspace_id = @workspace_id
  AND b.created_by_agent_id = @agent_id
  AND b.created_at >= @period_start::timestamptz
  AND b.created_at < @period_end::timestamptz;

-- name: ListAllWorkspaceIDs :many
-- Every workspace, for the nightly scorecard job.
SELECT id FROM workspace ORDER BY created_at ASC, id ASC;

-- name: GetScorecardInWorkspace :one
SELECT * FROM scorecard WHERE id = @id AND workspace_id = @workspace_id;

-- name: DeleteWorkspaceScorecards :exec
-- Workspace teardown: scorecards have no foreign keys.
DELETE FROM scorecard WHERE workspace_id = @workspace_id;

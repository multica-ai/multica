-- The Secretary's digest: facts about the company, computed by SQL. Every query
-- filters by workspace_id (directly, or through the squad / agent it joins),
-- because there are no foreign keys to do it for us.
--
-- A squad's issues are the issues assigned to the squad itself
-- (assignee_type = 'squad'), the same set its leader is woken for.

-- name: DigestSquadStats :many
-- One row per live squad with the counts the digest ranks on. "done" is the
-- done lifecycle category (built-in `done` or a custom status in that
-- category), moved there since @since. "blocked" is the built-in `blocked`
-- status. "stale" is in_progress with no activity since @stale_before.
SELECT
    s.id AS squad_id,
    s.name,
    s.floor_id,
    count(i.id) FILTER (
        WHERE i.updated_at >= sqlc.arg('since')::timestamptz
          AND (i.status = 'done' OR st.category = 'done')
    )::int AS done_count,
    count(i.id) FILTER (WHERE i.status = 'in_progress')::int AS in_progress_count,
    count(i.id) FILTER (WHERE i.status = 'blocked')::int AS blocked_count,
    count(i.id) FILTER (
        WHERE i.status = 'in_progress'
          AND COALESCE(i.last_activity_at, i.updated_at) < sqlc.arg('stale_before')::timestamptz
    )::int AS stale_count,
    (SELECT count(*) FROM agent_task_queue t
      WHERE t.squad_id = s.id
        AND t.status = 'failed'
        AND t.completed_at >= sqlc.arg('since')::timestamptz)::int AS failed_runs_count,
    (SELECT count(*) FROM activity_log al
       JOIN issue ai ON ai.id = al.issue_id AND ai.workspace_id = s.workspace_id
      WHERE al.workspace_id = s.workspace_id
        AND ai.assignee_type = 'squad' AND ai.assignee_id = s.id
        AND al.action = 'squad_leader_evaluated'
        AND al.details->>'outcome' = 'no_action'
        AND al.created_at >= sqlc.arg('since')::timestamptz)::int AS leader_no_action_count,
    (SELECT count(*) FROM proposal p
      WHERE p.workspace_id = s.workspace_id AND p.squad_id = s.id AND p.state = 'pending')::int AS pending_proposal_count
FROM squad s
LEFT JOIN issue i
       ON i.workspace_id = s.workspace_id
      AND i.assignee_type = 'squad'
      AND i.assignee_id = s.id
LEFT JOIN issue_status st
       ON st.workspace_id = i.workspace_id AND st.key = i.status
WHERE s.workspace_id = sqlc.arg('workspace_id')::uuid
  AND s.archived_at IS NULL
GROUP BY s.id, s.name, s.floor_id;

-- name: DigestPendingProposals :many
-- Pending proposals of the given squads, newest first, up to
-- @per_squad_limit per squad. The title is the backing issue's.
SELECT squad_id, proposal_id, title
FROM (
    SELECT p.squad_id, p.id AS proposal_id, i.title,
           row_number() OVER (PARTITION BY p.squad_id ORDER BY p.created_at DESC, p.id DESC) AS rn
    FROM proposal p
    JOIN issue i ON i.id = p.issue_id AND i.workspace_id = p.workspace_id
    WHERE p.workspace_id = sqlc.arg('workspace_id')::uuid
      AND p.state = 'pending'
      AND p.squad_id = ANY(sqlc.arg('squad_ids')::uuid[])
) ranked
WHERE rn <= sqlc.arg('per_squad_limit')::int
ORDER BY squad_id, rn;

-- name: DigestRecentDoneIssues :many
-- The most recent issues each of the given squads completed since @since, up
-- to @per_squad_limit per squad, for the digest's mention links.
SELECT squad_id, issue_id, number, title
FROM (
    SELECT i.assignee_id AS squad_id, i.id AS issue_id, i.number, i.title,
           row_number() OVER (PARTITION BY i.assignee_id ORDER BY i.updated_at DESC, i.id DESC) AS rn
    FROM issue i
    LEFT JOIN issue_status st
           ON st.workspace_id = i.workspace_id AND st.key = i.status
    WHERE i.workspace_id = sqlc.arg('workspace_id')::uuid
      AND i.assignee_type = 'squad'
      AND i.assignee_id = ANY(sqlc.arg('squad_ids')::uuid[])
      AND i.updated_at >= sqlc.arg('since')::timestamptz
      AND (i.status = 'done' OR st.category = 'done')
) ranked
WHERE rn <= sqlc.arg('per_squad_limit')::int
ORDER BY squad_id, rn;

-- name: DigestWorkspaceRuns :one
-- Runs that finished since @since, and how many of them failed.
SELECT
    count(*)::int AS runs,
    count(*) FILTER (WHERE t.status = 'failed')::int AS failed_runs
FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
WHERE a.workspace_id = sqlc.arg('workspace_id')::uuid
  AND t.status IN ('completed', 'failed')
  AND t.completed_at >= sqlc.arg('since')::timestamptz;

-- name: DigestWorkspaceUsage :one
-- Token totals and provider-reported cost (1e-10 USD) since @since, from the
-- hourly rollup. Runs without a provider-reported cost are priced client-side
-- from a rate table, so they add tokens here but nothing to the cost.
SELECT
    COALESCE(SUM(input_tokens), 0)::bigint AS input_tokens,
    COALESCE(SUM(output_tokens), 0)::bigint AS output_tokens,
    COALESCE(SUM(cost_usd_ticks), 0)::bigint AS cost_usd_ticks
FROM task_usage_hourly
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND bucket_hour >= date_trunc('hour', sqlc.arg('since')::timestamptz);

-- name: DigestOfflineRuntimes :many
-- Offline runtimes, most recently seen first. total_count repeats on each row.
SELECT id, COALESCE(NULLIF(custom_name, ''), name)::text AS name, provider,
       count(*) OVER ()::int AS total_count
FROM agent_runtime
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND status = 'offline'
ORDER BY last_seen_at DESC NULLS LAST, id
LIMIT sqlc.arg('row_limit')::int;

-- name: DigestAgentsWithFailedRuns :many
-- Agents whose runs failed since @since, worst first. total_count repeats on
-- each row.
SELECT a.id, a.name, count(*)::int AS failed_runs,
       count(*) OVER ()::int AS total_count
FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
WHERE a.workspace_id = sqlc.arg('workspace_id')::uuid
  AND t.status = 'failed'
  AND t.completed_at >= sqlc.arg('since')::timestamptz
GROUP BY a.id, a.name
ORDER BY count(*) DESC, a.name, a.id
LIMIT sqlc.arg('row_limit')::int;

-- name: DigestUnseatedSquads :many
-- Live squads with no seat on a floor. total_count repeats on each row.
SELECT id, name, count(*) OVER ()::int AS total_count
FROM squad
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND archived_at IS NULL
  AND (floor_id IS NULL OR office_slot IS NULL)
ORDER BY created_at, id
LIMIT sqlc.arg('row_limit')::int;

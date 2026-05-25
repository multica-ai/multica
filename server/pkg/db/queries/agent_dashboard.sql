-- name: GetAgentRunDashboardSummary :one
WITH filtered AS (
    SELECT
        atq.id,
        atq.agent_id,
        atq.status,
        atq.started_at,
        atq.completed_at,
        COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AS run_at
    FROM agent_task_queue atq
    JOIN agent a ON a.id = atq.agent_id
    WHERE a.workspace_id = @workspace_id
      AND COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) >= @since::timestamptz
      AND (COALESCE(cardinality(@agent_ids::uuid[]), 0) = 0 OR atq.agent_id = ANY(@agent_ids::uuid[]))
      AND (COALESCE(cardinality(@owner_ids::uuid[]), 0) = 0 OR a.owner_id = ANY(@owner_ids::uuid[]))
      AND (
        (@start_hour::int <= @end_hour::int AND EXTRACT(HOUR FROM COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AT TIME ZONE @tz::text)::int BETWEEN @start_hour::int AND @end_hour::int)
        OR (@start_hour::int > @end_hour::int AND (
          EXTRACT(HOUR FROM COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AT TIME ZONE @tz::text)::int >= @start_hour::int
          OR EXTRACT(HOUR FROM COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AT TIME ZONE @tz::text)::int <= @end_hour::int
        ))
      )
)
SELECT
    COUNT(*)::int AS total_runs,
    COUNT(*) FILTER (WHERE status = 'completed')::int AS successful_runs,
    COUNT(*) FILTER (WHERE status = 'failed')::int AS failed_runs,
    COUNT(DISTINCT agent_id)::int AS active_agent_count,
    COALESCE(
        AVG(EXTRACT(EPOCH FROM (completed_at - started_at))) FILTER (
            WHERE status IN ('completed', 'failed')
              AND started_at IS NOT NULL
              AND completed_at IS NOT NULL
        ),
        0
    )::double precision AS average_duration_seconds
FROM filtered;

-- name: ListAgentRunDashboardDaily :many
WITH filtered AS (
    SELECT
        atq.id,
        atq.agent_id,
        atq.status,
        COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AS run_at
    FROM agent_task_queue atq
    JOIN agent a ON a.id = atq.agent_id
    WHERE a.workspace_id = @workspace_id
      AND COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) >= @since::timestamptz
      AND (COALESCE(cardinality(@agent_ids::uuid[]), 0) = 0 OR atq.agent_id = ANY(@agent_ids::uuid[]))
      AND (COALESCE(cardinality(@owner_ids::uuid[]), 0) = 0 OR a.owner_id = ANY(@owner_ids::uuid[]))
      AND (
        (@start_hour::int <= @end_hour::int AND EXTRACT(HOUR FROM COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AT TIME ZONE @tz::text)::int BETWEEN @start_hour::int AND @end_hour::int)
        OR (@start_hour::int > @end_hour::int AND (
          EXTRACT(HOUR FROM COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AT TIME ZONE @tz::text)::int >= @start_hour::int
          OR EXTRACT(HOUR FROM COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AT TIME ZONE @tz::text)::int <= @end_hour::int
        ))
      )
)
SELECT
    DATE(run_at AT TIME ZONE @tz::text) AS date,
    COUNT(*)::int AS total_runs,
    COUNT(*) FILTER (WHERE status = 'completed')::int AS successful_runs,
    COUNT(*) FILTER (WHERE status = 'failed')::int AS failed_runs
FROM filtered
GROUP BY DATE(run_at AT TIME ZONE @tz::text)
ORDER BY DATE(run_at AT TIME ZONE @tz::text) ASC;

-- name: ListAgentRunDashboardHeatmap :many
WITH filtered AS (
    SELECT
        atq.id,
        COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AS run_at
    FROM agent_task_queue atq
    JOIN agent a ON a.id = atq.agent_id
    WHERE a.workspace_id = @workspace_id
      AND COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) >= @since::timestamptz
      AND (COALESCE(cardinality(@agent_ids::uuid[]), 0) = 0 OR atq.agent_id = ANY(@agent_ids::uuid[]))
      AND (COALESCE(cardinality(@owner_ids::uuid[]), 0) = 0 OR a.owner_id = ANY(@owner_ids::uuid[]))
      AND (
        (@start_hour::int <= @end_hour::int AND EXTRACT(HOUR FROM COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AT TIME ZONE @tz::text)::int BETWEEN @start_hour::int AND @end_hour::int)
        OR (@start_hour::int > @end_hour::int AND (
          EXTRACT(HOUR FROM COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AT TIME ZONE @tz::text)::int >= @start_hour::int
          OR EXTRACT(HOUR FROM COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AT TIME ZONE @tz::text)::int <= @end_hour::int
        ))
      )
)
SELECT
    EXTRACT(DOW FROM run_at AT TIME ZONE @tz::text)::int AS weekday,
    EXTRACT(HOUR FROM run_at AT TIME ZONE @tz::text)::int AS hour,
    COUNT(*)::int AS run_count
FROM filtered
GROUP BY
    EXTRACT(DOW FROM run_at AT TIME ZONE @tz::text),
    EXTRACT(HOUR FROM run_at AT TIME ZONE @tz::text)
ORDER BY weekday ASC, hour ASC;

-- name: ListAgentRunDashboardFailureReasons :many
WITH filtered AS (
    SELECT
        atq.id,
        atq.status,
        atq.failure_reason,
        atq.error,
        COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AS run_at
    FROM agent_task_queue atq
    JOIN agent a ON a.id = atq.agent_id
    WHERE a.workspace_id = @workspace_id
      AND atq.status = 'failed'
      AND COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) >= @since::timestamptz
      AND (COALESCE(cardinality(@agent_ids::uuid[]), 0) = 0 OR atq.agent_id = ANY(@agent_ids::uuid[]))
      AND (COALESCE(cardinality(@owner_ids::uuid[]), 0) = 0 OR a.owner_id = ANY(@owner_ids::uuid[]))
      AND (
        (@start_hour::int <= @end_hour::int AND EXTRACT(HOUR FROM COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AT TIME ZONE @tz::text)::int BETWEEN @start_hour::int AND @end_hour::int)
        OR (@start_hour::int > @end_hour::int AND (
          EXTRACT(HOUR FROM COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AT TIME ZONE @tz::text)::int >= @start_hour::int
          OR EXTRACT(HOUR FROM COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AT TIME ZONE @tz::text)::int <= @end_hour::int
        ))
      )
),
classified AS (
    SELECT
        CASE
            WHEN COALESCE(NULLIF(failure_reason, ''), '') <> '' THEN failure_reason
            WHEN error ILIKE '%503%' OR error ILIKE '%service unavailable%' THEN 'http_503'
            WHEN error ILIKE '%timeout%' OR error ILIKE '%deadline%' THEN 'timeout'
            WHEN error ILIKE '%permission%' OR error ILIKE '%forbidden%' OR error ILIKE '%unauthorized%' THEN 'permission'
            WHEN error ILIKE '%invalid%' OR error ILIKE '%400%' OR error ILIKE '%parameter%' THEN 'invalid_request'
            ELSE 'agent_error'
        END AS reason
    FROM filtered
)
SELECT reason, COUNT(*)::int AS count
FROM classified
GROUP BY reason
ORDER BY count DESC, reason ASC;

-- name: ListAgentRunDashboardAgents :many
WITH filtered AS (
    SELECT
        atq.id,
        atq.agent_id,
        atq.issue_id,
        atq.status,
        atq.started_at,
        atq.completed_at,
        COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AS run_at
    FROM agent_task_queue atq
    JOIN agent a ON a.id = atq.agent_id
    WHERE a.workspace_id = @workspace_id
      AND COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) >= @since::timestamptz
      AND (COALESCE(cardinality(@agent_ids::uuid[]), 0) = 0 OR atq.agent_id = ANY(@agent_ids::uuid[]))
      AND (COALESCE(cardinality(@owner_ids::uuid[]), 0) = 0 OR a.owner_id = ANY(@owner_ids::uuid[]))
      AND (
        (@start_hour::int <= @end_hour::int AND EXTRACT(HOUR FROM COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AT TIME ZONE @tz::text)::int BETWEEN @start_hour::int AND @end_hour::int)
        OR (@start_hour::int > @end_hour::int AND (
          EXTRACT(HOUR FROM COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AT TIME ZONE @tz::text)::int >= @start_hour::int
          OR EXTRACT(HOUR FROM COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AT TIME ZONE @tz::text)::int <= @end_hour::int
        ))
      )
),
per_agent AS (
    SELECT
        f.agent_id,
        COUNT(*)::int AS total_runs,
        COUNT(*) FILTER (WHERE f.status = 'completed')::int AS successful_runs,
        COUNT(*) FILTER (WHERE f.status = 'failed')::int AS failed_runs,
        COALESCE(
            AVG(EXTRACT(EPOCH FROM (f.completed_at - f.started_at))) FILTER (
                WHERE f.status IN ('completed', 'failed')
                  AND f.started_at IS NOT NULL
                  AND f.completed_at IS NOT NULL
            ),
            0
        )::double precision AS average_duration_seconds,
        MAX(f.run_at) AS last_run_at,
        COUNT(DISTINCT i.project_id) FILTER (WHERE i.project_id IS NOT NULL)::int AS project_count
    FROM filtered f
    LEFT JOIN issue i ON i.id = f.issue_id
    GROUP BY f.agent_id
),
latest AS (
    SELECT DISTINCT ON (f.agent_id)
        f.agent_id,
        f.id AS last_task_id,
        f.status AS last_status,
        f.run_at AS last_run_at,
        i.project_id,
        p.title AS project_title
    FROM filtered f
    LEFT JOIN issue i ON i.id = f.issue_id
    LEFT JOIN project p ON p.id = i.project_id
    ORDER BY f.agent_id, f.run_at DESC, f.id DESC
)
SELECT
    a.id AS agent_id,
    a.name AS agent_name,
    a.status AS agent_status,
    COALESCE(pa.total_runs, 0)::int AS total_runs,
    COALESCE(pa.successful_runs, 0)::int AS successful_runs,
    COALESCE(pa.failed_runs, 0)::int AS failed_runs,
    COALESCE(pa.average_duration_seconds, 0)::double precision AS average_duration_seconds,
    pa.last_run_at::timestamptz AS last_run_at,
    latest.last_task_id,
    latest.last_status,
    latest.project_id,
    latest.project_title,
    COALESCE(pa.project_count, 0)::int AS project_count
FROM agent a
LEFT JOIN per_agent pa ON pa.agent_id = a.id
LEFT JOIN latest ON latest.agent_id = a.id
WHERE a.workspace_id = @workspace_id
  AND a.archived_at IS NULL
  AND (COALESCE(cardinality(@agent_ids::uuid[]), 0) = 0 OR a.id = ANY(@agent_ids::uuid[]))
  AND (COALESCE(cardinality(@owner_ids::uuid[]), 0) = 0 OR a.owner_id = ANY(@owner_ids::uuid[]))
ORDER BY a.name ASC;

-- name: ListAgentRunDashboardRecentRuns :many
WITH filtered AS (
    SELECT
        atq.id,
        atq.agent_id,
        atq.issue_id,
        atq.status,
        atq.started_at,
        atq.completed_at,
        atq.created_at,
        atq.failure_reason,
        atq.error,
        atq.attempt,
        atq.max_attempts,
        COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AS run_at
    FROM agent_task_queue atq
    JOIN agent a ON a.id = atq.agent_id
    WHERE a.workspace_id = @workspace_id
      AND COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) >= @since::timestamptz
      AND (COALESCE(cardinality(@agent_ids::uuid[]), 0) = 0 OR atq.agent_id = ANY(@agent_ids::uuid[]))
      AND (COALESCE(cardinality(@owner_ids::uuid[]), 0) = 0 OR a.owner_id = ANY(@owner_ids::uuid[]))
      AND (
        (@start_hour::int <= @end_hour::int AND EXTRACT(HOUR FROM COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AT TIME ZONE @tz::text)::int BETWEEN @start_hour::int AND @end_hour::int)
        OR (@start_hour::int > @end_hour::int AND (
          EXTRACT(HOUR FROM COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AT TIME ZONE @tz::text)::int >= @start_hour::int
          OR EXTRACT(HOUR FROM COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AT TIME ZONE @tz::text)::int <= @end_hour::int
        ))
      )
)
SELECT
    f.id,
    f.agent_id,
    a.name AS agent_name,
    f.issue_id,
    i.title AS issue_title,
    i.number AS issue_number,
    i.project_id,
    p.title AS project_title,
    f.status,
    f.run_at,
    f.started_at,
    f.completed_at,
    COALESCE(EXTRACT(EPOCH FROM (f.completed_at - f.started_at)), 0)::double precision AS duration_seconds,
    CASE
        WHEN f.status <> 'failed' THEN ''
        WHEN COALESCE(NULLIF(f.failure_reason, ''), '') <> '' THEN f.failure_reason
        WHEN f.error ILIKE '%503%' OR f.error ILIKE '%service unavailable%' THEN 'http_503'
        WHEN f.error ILIKE '%timeout%' OR f.error ILIKE '%deadline%' THEN 'timeout'
        WHEN f.error ILIKE '%permission%' OR f.error ILIKE '%forbidden%' OR f.error ILIKE '%unauthorized%' THEN 'permission'
        WHEN f.error ILIKE '%invalid%' OR f.error ILIKE '%400%' OR f.error ILIKE '%parameter%' THEN 'invalid_request'
        ELSE 'agent_error'
    END AS failure_reason,
    f.error,
    f.attempt,
    f.max_attempts
FROM filtered f
JOIN agent a ON a.id = f.agent_id
LEFT JOIN issue i ON i.id = f.issue_id
LEFT JOIN project p ON p.id = i.project_id
WHERE (@failed_only::boolean IS FALSE OR f.status = 'failed')
ORDER BY f.run_at DESC, f.id DESC
LIMIT @limit_count::int;

-- name: ListAgentRunDashboardRetryDistribution :many
WITH filtered AS (
    SELECT
        atq.id,
        atq.agent_id,
        atq.attempt,
        COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AS run_at
    FROM agent_task_queue atq
    JOIN agent a ON a.id = atq.agent_id
    WHERE a.workspace_id = @workspace_id
      AND COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) >= @since::timestamptz
      AND (COALESCE(cardinality(@agent_ids::uuid[]), 0) = 0 OR atq.agent_id = ANY(@agent_ids::uuid[]))
      AND (COALESCE(cardinality(@owner_ids::uuid[]), 0) = 0 OR a.owner_id = ANY(@owner_ids::uuid[]))
      AND (
        (@start_hour::int <= @end_hour::int AND EXTRACT(HOUR FROM COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AT TIME ZONE @tz::text)::int BETWEEN @start_hour::int AND @end_hour::int)
        OR (@start_hour::int > @end_hour::int AND (
          EXTRACT(HOUR FROM COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AT TIME ZONE @tz::text)::int >= @start_hour::int
          OR EXTRACT(HOUR FROM COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AT TIME ZONE @tz::text)::int <= @end_hour::int
        ))
      )
)
SELECT attempt, COUNT(*)::int AS count
FROM filtered
GROUP BY attempt
ORDER BY attempt ASC;

-- name: GetAgentRunDashboardRun :one
SELECT
    atq.id,
    atq.agent_id,
    a.name AS agent_name,
    atq.issue_id,
    i.title AS issue_title,
    i.number AS issue_number,
    i.project_id,
    p.title AS project_title,
    atq.status,
    atq.priority,
    atq.created_at,
    atq.dispatched_at,
    atq.started_at,
    atq.completed_at,
    COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) AS run_at,
    COALESCE(EXTRACT(EPOCH FROM (atq.completed_at - atq.started_at)), 0)::double precision AS duration_seconds,
    atq.result,
    atq.error,
    atq.failure_reason,
    atq.attempt,
    atq.max_attempts,
    atq.parent_task_id
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
LEFT JOIN issue i ON i.id = atq.issue_id
LEFT JOIN project p ON p.id = i.project_id
WHERE atq.id = @task_id
  AND a.workspace_id = @workspace_id;

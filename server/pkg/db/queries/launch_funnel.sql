-- name: ListLaunchFunnelEvents :many
-- Canonical workspace-cohort event stream for internal Product/Data reporting.
-- Acquisition events remain in PostHog because they precede an account; every
-- event below is derived from durable operational rows and is never emitted as
-- a second analytics copy. excluded_workspace_ids is the validation/test cohort
-- deny-list supplied by the dashboard job.
WITH workspace_cohort AS (
    SELECT
        w.id AS workspace_id,
        w.created_at AS workspace_created_at,
        COALESCE(owner_user.acquisition_attribution->>'source', 'direct')::text AS source,
        COALESCE(owner_user.acquisition_attribution->>'medium', 'none')::text AS medium,
        COALESCE(owner_user.acquisition_attribution->>'campaign', 'none')::text AS campaign
    FROM workspace w
    LEFT JOIN LATERAL (
        SELECT u.acquisition_attribution
        FROM member m
        JOIN "user" u ON u.id = m.user_id
        WHERE m.workspace_id = w.id
          AND m.role = 'owner'
        ORDER BY m.created_at ASC, m.id ASC
        LIMIT 1
    ) owner_user ON true
    WHERE w.created_at >= sqlc.arg('from_time')::timestamptz
      AND w.created_at < sqlc.arg('to_time')::timestamptz
      AND (
          COALESCE(cardinality(sqlc.arg('excluded_workspace_ids')::uuid[]), 0) = 0
          OR NOT (w.id = ANY(sqlc.arg('excluded_workspace_ids')::uuid[]))
      )
),
first_runtime AS (
    SELECT DISTINCT ON (ar.workspace_id)
        ar.workspace_id,
        ar.id AS runtime_id,
        ar.created_at AS connected_at,
        ar.runtime_mode,
        ar.provider
    FROM agent_runtime ar
    JOIN workspace_cohort wc ON wc.workspace_id = ar.workspace_id
    ORDER BY ar.workspace_id, ar.created_at ASC, ar.id ASC
),
first_agent AS (
    SELECT DISTINCT ON (a.workspace_id)
        a.workspace_id,
        a.id AS agent_id,
        a.created_at,
        ar.runtime_mode,
        ar.provider
    FROM agent a
    JOIN workspace_cohort wc ON wc.workspace_id = a.workspace_id
    LEFT JOIN agent_runtime ar ON ar.id = a.runtime_id
    ORDER BY a.workspace_id, a.created_at ASC, a.id ASC
),
issue_assignments AS (
    SELECT
        i.workspace_id,
        atq.issue_id,
        MIN(atq.created_at) AS assigned_at
    FROM agent_task_queue atq
    JOIN issue i ON i.id = atq.issue_id
    JOIN workspace_cohort wc ON wc.workspace_id = i.workspace_id
    GROUP BY i.workspace_id, atq.issue_id
),
ranked_issue_assignments AS (
    SELECT
        ia.workspace_id,
        ia.issue_id,
        ia.assigned_at,
        ROW_NUMBER() OVER (
            PARTITION BY ia.workspace_id
            ORDER BY ia.assigned_at ASC, ia.issue_id ASC
        ) AS assignment_ordinal
    FROM issue_assignments ia
),
first_assignment AS (
    SELECT
        ria.workspace_id,
        ria.issue_id,
        ria.assigned_at,
        task.runtime_mode,
        task.provider
    FROM ranked_issue_assignments ria
    LEFT JOIN LATERAL (
        SELECT ar.runtime_mode, ar.provider
        FROM agent_task_queue atq
        LEFT JOIN agent_runtime ar ON ar.id = atq.runtime_id
        WHERE atq.issue_id = ria.issue_id
        ORDER BY atq.created_at ASC, atq.id ASC
        LIMIT 1
    ) task ON true
    WHERE ria.assignment_ordinal = 1
),
first_task_started AS (
    SELECT
        fa.workspace_id,
        MIN(atq.started_at) AS started_at
    FROM first_assignment fa
    JOIN agent_task_queue atq ON atq.issue_id = fa.issue_id
    WHERE atq.started_at IS NOT NULL
    GROUP BY fa.workspace_id
),
first_issue_review AS (
    SELECT
        fa.workspace_id,
        MIN(al.created_at) AS reviewed_at
    FROM first_assignment fa
    JOIN activity_log al ON al.issue_id = fa.issue_id
    WHERE al.action = 'status_changed'
      AND al.details->>'to' = 'in_review'
      AND al.created_at >= fa.assigned_at
    GROUP BY fa.workspace_id
),
second_assignment AS (
    SELECT workspace_id, issue_id, assigned_at
    FROM ranked_issue_assignments
    WHERE assignment_ordinal = 2
),
events AS (
    SELECT
        concat('workspace_created:', wc.workspace_id)::text AS event_id,
        'workspace_created'::text AS event_name,
        wc.workspace_id,
        wc.workspace_created_at AS occurred_at,
        wc.source,
        wc.medium,
        wc.campaign,
        'unknown'::text AS platform,
        NULL::text AS runtime_family,
        NULL::text AS provider_family,
        NULL::text AS failure_reason
    FROM workspace_cohort wc

    UNION ALL

    SELECT
        concat('runtime_connected:', wc.workspace_id)::text,
        'runtime_connected',
        wc.workspace_id,
        fr.connected_at,
        wc.source,
        wc.medium,
        wc.campaign,
        'unknown',
        fr.runtime_mode,
        fr.provider,
        NULL::text
    FROM workspace_cohort wc
    JOIN first_runtime fr ON fr.workspace_id = wc.workspace_id

    UNION ALL

    SELECT
        concat('agent_created:', wc.workspace_id)::text,
        'agent_created',
        wc.workspace_id,
        fa.created_at,
        wc.source,
        wc.medium,
        wc.campaign,
        'unknown',
        fa.runtime_mode,
        fa.provider,
        NULL::text
    FROM workspace_cohort wc
    JOIN first_agent fa ON fa.workspace_id = wc.workspace_id

    UNION ALL

    SELECT
        concat('issue_assigned:', wc.workspace_id)::text,
        'issue_assigned',
        wc.workspace_id,
        fa.assigned_at,
        wc.source,
        wc.medium,
        wc.campaign,
        'unknown',
        fa.runtime_mode,
        fa.provider,
        NULL::text
    FROM workspace_cohort wc
    JOIN first_assignment fa ON fa.workspace_id = wc.workspace_id

    UNION ALL

    SELECT
        concat('task_started:', wc.workspace_id)::text,
        'task_started',
        wc.workspace_id,
        fts.started_at,
        wc.source,
        wc.medium,
        wc.campaign,
        'unknown',
        fa.runtime_mode,
        fa.provider,
        NULL::text
    FROM workspace_cohort wc
    JOIN first_assignment fa ON fa.workspace_id = wc.workspace_id
    JOIN first_task_started fts ON fts.workspace_id = wc.workspace_id

    UNION ALL

    SELECT
        concat('issue_in_review:', wc.workspace_id)::text,
        'issue_in_review',
        wc.workspace_id,
        fir.reviewed_at,
        wc.source,
        wc.medium,
        wc.campaign,
        'unknown',
        fa.runtime_mode,
        fa.provider,
        NULL::text
    FROM workspace_cohort wc
    JOIN first_assignment fa ON fa.workspace_id = wc.workspace_id
    JOIN first_issue_review fir ON fir.workspace_id = wc.workspace_id

    UNION ALL

    SELECT
        concat('second_issue_assigned:', wc.workspace_id)::text,
        'second_issue_assigned',
        wc.workspace_id,
        sa.assigned_at,
        wc.source,
        wc.medium,
        wc.campaign,
        'unknown',
        fa.runtime_mode,
        fa.provider,
        NULL::text
    FROM workspace_cohort wc
    JOIN first_assignment fa ON fa.workspace_id = wc.workspace_id
    JOIN second_assignment sa ON sa.workspace_id = wc.workspace_id
)
SELECT *
FROM events
ORDER BY occurred_at ASC, event_name ASC;

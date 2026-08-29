-- name: GetOperationalControlsSummary :one
WITH approval_state AS (
    SELECT
        count(*) FILTER (WHERE approval.status = 'pending' AND approval.expires_at > @as_of)::bigint AS pending_current
    FROM agent_tool_approval_request AS approval
    WHERE approval.workspace_id = @workspace_id
),
action_window AS (
    SELECT
        count(*) FILTER (WHERE event.event_type = 'approval_approved')::bigint AS approved_count,
        count(*) FILTER (WHERE event.event_type = 'approval_denied')::bigint AS denied_count,
        count(*) FILTER (WHERE event.event_type = 'approval_expired')::bigint AS expired_count,
        count(*) FILTER (WHERE event.event_type = 'failed')::bigint AS failed_count,
        count(DISTINCT (event.task_id, event.invocation_id)) FILTER (
            WHERE event.coverage_kind IN ('managed_mcp', 'managed_native')
              AND event.event_type IN ('started', 'succeeded', 'failed', 'cancelled')
        )::bigint AS intercepted_invocation_count,
        count(DISTINCT (event.task_id, event.invocation_id)) FILTER (
            WHERE event.coverage_kind = 'declaration_only'
        )::bigint AS declaration_only_count
    FROM agent_tool_action_event AS event
    WHERE event.workspace_id = @workspace_id
      AND event.created_at >= @window_start
      AND event.created_at < @window_end
),
decision_window AS (
    SELECT percentile_cont(0.5) WITHIN GROUP (
        ORDER BY EXTRACT(EPOCH FROM (approval.decided_at - approval.requested_at)) * 1000
    )::double precision AS median_decision_ms
    FROM agent_tool_approval_request AS approval
    WHERE approval.workspace_id = @workspace_id
      AND approval.decided_by_user_id IS NOT NULL
      AND approval.decided_at >= @window_start
      AND approval.decided_at < @window_end
),
policy_state AS (
    SELECT count(*) FILTER (WHERE policy.status = 'active')::bigint AS active_policy_count
    FROM agent_tool_policy AS policy
    WHERE policy.workspace_id = @workspace_id
)
SELECT
    approval_state.pending_current,
    action_window.approved_count,
    action_window.denied_count,
    action_window.expired_count,
    action_window.failed_count,
    action_window.intercepted_invocation_count,
    action_window.declaration_only_count,
    decision_window.median_decision_ms,
    policy_state.active_policy_count
FROM approval_state, action_window, decision_window, policy_state;

-- name: ListOperationalControlsByAgent :many
WITH agent_ids AS (
    SELECT agent_id FROM agent_tool_policy
    WHERE workspace_id = @workspace_id
    UNION
    SELECT agent_id FROM agent_tool_approval_request
    WHERE workspace_id = @workspace_id
    UNION
    SELECT agent_id FROM agent_tool_action_event
    WHERE workspace_id = @workspace_id
)
SELECT
    agent_ids.agent_id,
    (
        SELECT count(*)::bigint
        FROM agent_tool_approval_request AS approval
        WHERE approval.workspace_id = @workspace_id
          AND approval.agent_id = agent_ids.agent_id
          AND approval.status = 'pending'
          AND approval.expires_at > @as_of
    ) AS pending_current,
    (
        SELECT count(*)::bigint
        FROM agent_tool_action_event AS event
        WHERE event.workspace_id = @workspace_id
          AND event.agent_id = agent_ids.agent_id
          AND event.created_at >= @window_start
          AND event.created_at < @window_end
          AND event.event_type = 'approval_approved'
    ) AS approved_count,
    (
        SELECT count(*)::bigint
        FROM agent_tool_action_event AS event
        WHERE event.workspace_id = @workspace_id
          AND event.agent_id = agent_ids.agent_id
          AND event.created_at >= @window_start
          AND event.created_at < @window_end
          AND event.event_type = 'approval_denied'
    ) AS denied_count,
    (
        SELECT count(*)::bigint
        FROM agent_tool_action_event AS event
        WHERE event.workspace_id = @workspace_id
          AND event.agent_id = agent_ids.agent_id
          AND event.created_at >= @window_start
          AND event.created_at < @window_end
          AND event.event_type = 'failed'
    ) AS failed_count,
    (
        SELECT count(DISTINCT (event.task_id, event.invocation_id))::bigint
        FROM agent_tool_action_event AS event
        WHERE event.workspace_id = @workspace_id
          AND event.agent_id = agent_ids.agent_id
          AND event.created_at >= @window_start
          AND event.created_at < @window_end
          AND event.coverage_kind IN ('managed_mcp', 'managed_native')
          AND event.event_type IN ('started', 'succeeded', 'failed', 'cancelled')
    ) AS intercepted_invocation_count,
    (
        SELECT count(DISTINCT (event.task_id, event.invocation_id))::bigint
        FROM agent_tool_action_event AS event
        WHERE event.workspace_id = @workspace_id
          AND event.agent_id = agent_ids.agent_id
          AND event.created_at >= @window_start
          AND event.created_at < @window_end
          AND event.coverage_kind = 'declaration_only'
    ) AS declaration_only_count
FROM agent_ids
WHERE agent_ids.agent_id > COALESCE(sqlc.narg('after_agent_id')::uuid, '00000000-0000-0000-0000-000000000000'::uuid)
ORDER BY agent_ids.agent_id ASC
LIMIT @page_size::int;

-- name: ListOperationalControlsRecentEvents :many
SELECT * FROM agent_tool_action_event
WHERE workspace_id = @workspace_id
  AND created_at >= @window_start
  AND created_at < @window_end
  AND (
      sqlc.narg('cursor_created_at')::timestamptz IS NULL
      OR (created_at, id) < (
          sqlc.narg('cursor_created_at')::timestamptz,
          sqlc.narg('cursor_id')::uuid
      )
  )
ORDER BY created_at DESC, id DESC
LIMIT @page_size::int;

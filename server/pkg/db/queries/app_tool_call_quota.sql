-- name: IncrementAppToolCallQuota :one
-- Counts one app tool call against the workspace's period in one statement.
-- Under enforce the row only moves while used_count is below the limit, so
-- concurrent calls can never take a workspace past it; no row back means the
-- call is refused. Under observe every call is counted.
INSERT INTO app_tool_call_quota_period (workspace_id, period_start, period_end, used_count)
SELECT @workspace_id::uuid, @period_start::timestamptz, @period_end::timestamptz, 1
WHERE NOT @enforce::bool OR @limit_value::bigint > 0
ON CONFLICT (workspace_id, period_start, period_end) DO UPDATE
SET used_count = app_tool_call_quota_period.used_count + 1,
    updated_at = now()
WHERE NOT @enforce::bool OR app_tool_call_quota_period.used_count < @limit_value::bigint
RETURNING used_count;

-- name: GetAppToolCallQuotaUsed :one
SELECT used_count FROM app_tool_call_quota_period
WHERE workspace_id = $1 AND period_start = $2 AND period_end = $3;

-- name: ClaimAppToolCallQuotaRejectionNotice :one
-- Marks the period's limit notice as sent and returns a row only to the one
-- caller that should send it: the first rejection of the period, or with
-- per_limit the first at a limit not yet notified in the period. Run it in the
-- transaction that writes the Inbox items, so a failed delivery leaves the
-- marker unset for the next rejection.
INSERT INTO app_tool_call_quota_period (
    workspace_id, period_start, period_end, rejection_notified_at, rejection_notified_limits
) VALUES (@workspace_id, @period_start, @period_end, now(), ARRAY[@limit_value::bigint])
ON CONFLICT (workspace_id, period_start, period_end) DO UPDATE
SET rejection_notified_at = now(),
    rejection_notified_limits = array_append(app_tool_call_quota_period.rejection_notified_limits, @limit_value::bigint),
    updated_at = now()
WHERE app_tool_call_quota_period.rejection_notified_at IS NULL
   OR (@per_limit::bool
       AND NOT (@limit_value::bigint = ANY (app_tool_call_quota_period.rejection_notified_limits)))
RETURNING used_count;

-- name: ListAppToolCallUsageByMember :many
-- Calls counted in a period, per person who started the run or approved the
-- call. A NULL triggered_by is a run no person started.
SELECT triggered_by, count(*)::bigint AS calls
FROM app_tool_call
WHERE workspace_id = $1
  AND metered_period_start = sqlc.arg('metered_period_start')::timestamptz
GROUP BY triggered_by
ORDER BY calls DESC;

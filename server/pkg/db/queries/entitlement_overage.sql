-- name: RecordEntitlementLimitExceeded :one
-- One over-limit attempt. observed counts attempts that were admitted under
-- observe; otherwise the attempt was rejected under enforce.
INSERT INTO entitlement_limit_overage (
    workspace_id, gate, period_start, period_end, limit_value, peak_usage,
    observed_count, enforced_count, first_exceeded_at, last_exceeded_at
) VALUES (
    @workspace_id, @gate, @period_start, @period_end, @limit_value, @attempted_usage,
    CASE WHEN @observed::boolean THEN 1 ELSE 0 END,
    CASE WHEN @observed::boolean THEN 0 ELSE 1 END,
    now(), now()
)
ON CONFLICT (workspace_id, gate, period_start, period_end) DO UPDATE
SET limit_value = EXCLUDED.limit_value,
    peak_usage = GREATEST(entitlement_limit_overage.peak_usage, EXCLUDED.peak_usage),
    observed_count = entitlement_limit_overage.observed_count + EXCLUDED.observed_count,
    enforced_count = entitlement_limit_overage.enforced_count + EXCLUDED.enforced_count,
    last_exceeded_at = now(),
    updated_at = now()
RETURNING *;

-- name: ListWorkspaceEntitlementLimitOverages :many
-- Every recorded over-limit period of one workspace, newest period first.
SELECT * FROM entitlement_limit_overage
WHERE workspace_id = @workspace_id
ORDER BY period_start DESC, gate;

-- name: ListEntitlementLimitOveragesForPeriod :many
-- Workspaces that went over one gate's limit in a period. Rows exist only for
-- workspaces that went over, so the count is the reach of the limit.
SELECT * FROM entitlement_limit_overage
WHERE gate = @gate
  AND period_start = @period_start
  AND period_end = @period_end
ORDER BY peak_usage - limit_value DESC, workspace_id;

-- name: SyncEntitlementLimitOverageLimit :exec
-- Keeps an existing row's limit current when the period's limit changes
-- without a new over-limit attempt (an upgrade mid-period), so Over is not
-- reported against a stale limit. Counts and times are left as history.
UPDATE entitlement_limit_overage
SET limit_value = @limit_value,
    updated_at = now()
WHERE workspace_id = @workspace_id
  AND gate = @gate
  AND period_start = @period_start
  AND period_end = @period_end
  AND limit_value <> @limit_value;

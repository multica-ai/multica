-- name: RecordEntitlementFeatureAttempt :one
-- One attempt to add to a restricted paid feature. outcome is observed
-- (admitted under a configured observe), degraded (admitted because a stale
-- enforce was downgraded) or enforced (rejected).
INSERT INTO entitlement_feature_attempt (
    workspace_id, feature, day,
    observed_count, degraded_count, enforced_count,
    last_operation, last_action, last_reason,
    last_policy_revision, last_subscription_version,
    first_attempt_at, last_attempt_at
) VALUES (
    @workspace_id, @feature, (now() AT TIME ZONE 'UTC')::date,
    CASE WHEN @outcome::text = 'observed' THEN 1 ELSE 0 END,
    CASE WHEN @outcome::text = 'degraded' THEN 1 ELSE 0 END,
    CASE WHEN @outcome::text = 'enforced' THEN 1 ELSE 0 END,
    @operation, @action, @reason,
    @policy_revision, @subscription_version,
    now(), now()
)
ON CONFLICT (workspace_id, feature, day) DO UPDATE
SET observed_count = entitlement_feature_attempt.observed_count + EXCLUDED.observed_count,
    degraded_count = entitlement_feature_attempt.degraded_count + EXCLUDED.degraded_count,
    enforced_count = entitlement_feature_attempt.enforced_count + EXCLUDED.enforced_count,
    last_operation = EXCLUDED.last_operation,
    last_action = EXCLUDED.last_action,
    last_reason = EXCLUDED.last_reason,
    last_policy_revision = EXCLUDED.last_policy_revision,
    last_subscription_version = EXCLUDED.last_subscription_version,
    last_attempt_at = now(),
    updated_at = now()
RETURNING *;

-- name: ListWorkspaceEntitlementFeatureAttempts :many
-- Every recorded day of one workspace, newest first.
SELECT * FROM entitlement_feature_attempt
WHERE workspace_id = @workspace_id
ORDER BY day DESC, feature;

-- name: ListEntitlementFeatureAttemptsSince :many
-- Workspaces that tried to add to one feature from a UTC day on, one row per
-- workspace and day. Distinct workspaces are the reach of the restriction.
SELECT * FROM entitlement_feature_attempt
WHERE feature = @feature
  AND day >= @since_day
ORDER BY day DESC, workspace_id;

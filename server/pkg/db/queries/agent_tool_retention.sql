-- name: GetOperationalControlsRetentionDefaultDays :one
SELECT 90::integer AS retention_days
WHERE @workspace_id::uuid IS NOT NULL;

-- name: DeleteTerminalAgentToolApprovalRequestsByRetention :many
WITH retention_candidates AS MATERIALIZED (
    SELECT approval.id
    FROM agent_tool_approval_request AS approval
    WHERE approval.workspace_id = @workspace_id
      AND approval.status IN ('consumed', 'denied', 'expired', 'cancelled')
      AND COALESCE(approval.consumed_at, approval.decided_at) < @retention_cutoff
    ORDER BY COALESCE(approval.consumed_at, approval.decided_at) ASC, approval.id ASC
    LIMIT @batch_size::int
)
DELETE FROM agent_tool_approval_request AS approval
WHERE approval.workspace_id = @workspace_id
  AND approval.id IN (SELECT id FROM retention_candidates)
RETURNING approval.id;

-- name: DeleteAgentToolActionEventsByRetention :many
WITH retention_candidates AS MATERIALIZED (
    SELECT event.id
    FROM agent_tool_action_event AS event
    WHERE event.workspace_id = @workspace_id
      AND event.created_at < @retention_cutoff
    ORDER BY event.created_at ASC, event.id ASC
    LIMIT @batch_size::int
)
DELETE FROM agent_tool_action_event AS event
WHERE event.workspace_id = @workspace_id
  AND event.id IN (SELECT id FROM retention_candidates)
RETURNING event.id;

-- name: DeleteAgentToolActionEventsForWorkspace :execrows
DELETE FROM agent_tool_action_event
WHERE workspace_id = @workspace_id;

-- name: DeleteAgentToolApprovalRequestsForWorkspace :execrows
DELETE FROM agent_tool_approval_request
WHERE workspace_id = @workspace_id;

-- name: DeleteAgentToolPolicyRulesForWorkspaceCleanup :execrows
DELETE FROM agent_tool_policy_rule
WHERE workspace_id = @workspace_id;

-- name: DeleteAgentToolPolicyRevisionsForWorkspaceCleanup :execrows
DELETE FROM agent_tool_policy_revision
WHERE workspace_id = @workspace_id;

-- name: DeleteAgentToolPoliciesForWorkspaceCleanup :execrows
DELETE FROM agent_tool_policy
WHERE workspace_id = @workspace_id;

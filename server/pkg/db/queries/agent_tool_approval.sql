-- name: CreateOrGetAgentToolApprovalRequest :one
WITH inserted AS (
    INSERT INTO agent_tool_approval_request (
        id, workspace_id, agent_id, task_id, issue_id, chat_session_id,
        invocation_id, idempotency_key, transport_kind, server_key,
        tool_name, schema_digest, policy_revision, schema_field_names,
        argument_bytes, requested_at, expires_at
    ) VALUES (
        COALESCE(sqlc.narg('id')::uuid, gen_random_uuid()),
        @workspace_id, @agent_id, @task_id,
        sqlc.narg('issue_id')::uuid, sqlc.narg('chat_session_id')::uuid,
        @invocation_id, @idempotency_key, @transport_kind, @server_key,
        @tool_name, @schema_digest, @policy_revision,
        @schema_field_names::text[], @argument_bytes,
        @requested_at, @expires_at
    )
    ON CONFLICT DO NOTHING
    RETURNING *
)
SELECT * FROM inserted
UNION ALL
SELECT existing.*
FROM agent_tool_approval_request AS existing
WHERE NOT EXISTS (SELECT 1 FROM inserted)
  AND existing.workspace_id = @workspace_id
  AND existing.agent_id = @agent_id
  AND existing.task_id = @task_id
  AND existing.issue_id IS NOT DISTINCT FROM sqlc.narg('issue_id')::uuid
  AND existing.chat_session_id IS NOT DISTINCT FROM sqlc.narg('chat_session_id')::uuid
  AND existing.invocation_id = @invocation_id
  AND existing.idempotency_key = @idempotency_key
  AND existing.transport_kind = @transport_kind
  AND existing.server_key = @server_key
  AND existing.tool_name = @tool_name
  AND existing.schema_digest = @schema_digest
  AND existing.policy_revision = @policy_revision
  AND existing.schema_field_names = @schema_field_names::text[]
  AND existing.argument_bytes = @argument_bytes
  AND existing.expires_at = @expires_at
LIMIT 1;

-- name: GetAgentToolApprovalRequest :one
SELECT * FROM agent_tool_approval_request
WHERE workspace_id = @workspace_id AND id = @id;

-- name: GetAgentToolApprovalRequestForInvocation :one
SELECT * FROM agent_tool_approval_request
WHERE workspace_id = @workspace_id
  AND task_id = @task_id
  AND invocation_id = @invocation_id;

-- name: LockAgentToolApprovalRequest :one
SELECT * FROM agent_tool_approval_request
WHERE workspace_id = @workspace_id AND id = @id
FOR UPDATE;

-- name: ListPendingAgentToolApprovalRequests :many
SELECT * FROM agent_tool_approval_request
WHERE workspace_id = @workspace_id
  AND status = 'pending'
  AND expires_at > @as_of
  AND (
      sqlc.narg('filter_agent_id')::uuid IS NULL
      OR agent_id = sqlc.narg('filter_agent_id')::uuid
  )
  AND (
      sqlc.narg('cursor_requested_at')::timestamptz IS NULL
      OR (requested_at, id) > (
          sqlc.narg('cursor_requested_at')::timestamptz,
          sqlc.narg('cursor_id')::uuid
      )
  )
ORDER BY requested_at ASC, id ASC
LIMIT @page_size::int;

-- name: ListAgentToolApprovalRequestHistory :many
SELECT * FROM agent_tool_approval_request
WHERE workspace_id = @workspace_id
  AND agent_id = @agent_id
  AND (
      sqlc.narg('cursor_requested_at')::timestamptz IS NULL
      OR (requested_at, id) < (
          sqlc.narg('cursor_requested_at')::timestamptz,
          sqlc.narg('cursor_id')::uuid
      )
  )
ORDER BY requested_at DESC, id DESC
LIMIT @page_size::int;

-- name: ListExpiredAgentToolApprovalRequestsForUpdate :many
SELECT * FROM agent_tool_approval_request
WHERE workspace_id = @workspace_id
  AND status IN ('pending', 'approved')
  AND expires_at <= @as_of
ORDER BY expires_at ASC, id ASC
LIMIT @batch_size::int
FOR UPDATE SKIP LOCKED;

-- name: ApproveAgentToolApprovalRequest :one
UPDATE agent_tool_approval_request
SET status = 'approved',
    reason_code = 'operator_approved',
    decided_at = @decided_at,
    decided_by_user_id = @decided_by_user_id
WHERE workspace_id = @workspace_id
  AND id = @id
  AND agent_id = @agent_id
  AND task_id = @task_id
  AND invocation_id = @invocation_id
  AND transport_kind = @transport_kind
  AND server_key = @server_key
  AND tool_name = @tool_name
  AND schema_digest = @schema_digest
  AND policy_revision = @policy_revision
  AND status = 'pending'
  AND expires_at > @decided_at
RETURNING *;

-- name: DenyAgentToolApprovalRequest :one
UPDATE agent_tool_approval_request
SET status = 'denied',
    reason_code = @reason_code,
    decided_at = @decided_at,
    decided_by_user_id = @decided_by_user_id
WHERE workspace_id = @workspace_id
  AND id = @id
  AND agent_id = @agent_id
  AND task_id = @task_id
  AND invocation_id = @invocation_id
  AND transport_kind = @transport_kind
  AND server_key = @server_key
  AND tool_name = @tool_name
  AND schema_digest = @schema_digest
  AND policy_revision = @policy_revision
  AND status = 'pending'
  AND expires_at > @decided_at
RETURNING *;

-- name: ConsumeAgentToolApprovalRequest :one
UPDATE agent_tool_approval_request AS approval
SET status = 'consumed', consumed_at = @consumed_at
WHERE approval.workspace_id = @workspace_id
  AND approval.id = @id
  AND approval.agent_id = @agent_id
  AND approval.task_id = @task_id
  AND approval.invocation_id = @invocation_id
  AND approval.transport_kind = @transport_kind
  AND approval.server_key = @server_key
  AND approval.tool_name = @tool_name
  AND approval.schema_digest = @schema_digest
  AND approval.policy_revision = @policy_revision
  AND approval.status = 'approved'
  AND approval.expires_at > @consumed_at
  AND EXISTS (
      SELECT 1
      FROM agent_tool_policy AS policy
      JOIN agent_tool_policy_rule AS rule
        ON rule.workspace_id = policy.workspace_id
       AND rule.agent_id = policy.agent_id
       AND rule.policy_id = policy.id
      WHERE policy.workspace_id = @workspace_id
        AND policy.agent_id = @agent_id
        AND policy.status = 'active'
        AND policy.revision = @policy_revision
        AND rule.workspace_id = @workspace_id
        AND rule.transport_kind = @transport_kind
        AND rule.server_key = @server_key
        AND rule.tool_name = @tool_name
        AND rule.schema_digest = @schema_digest
        AND rule.effect = 'require_approval'
  )
RETURNING approval.*;

-- name: ExpireAgentToolApprovalRequest :one
UPDATE agent_tool_approval_request
SET status = 'expired',
    reason_code = 'request_expired',
    decided_at = COALESCE(decided_at, @expired_at)
WHERE workspace_id = @workspace_id
  AND id = @id
  AND agent_id = @agent_id
  AND task_id = @task_id
  AND invocation_id = @invocation_id
  AND transport_kind = @transport_kind
  AND server_key = @server_key
  AND tool_name = @tool_name
  AND schema_digest = @schema_digest
  AND policy_revision = @policy_revision
  AND status IN ('pending', 'approved')
  AND expires_at <= @expired_at
RETURNING *;

-- name: CancelAgentToolApprovalRequest :one
UPDATE agent_tool_approval_request
SET status = 'cancelled',
    reason_code = @reason_code,
    decided_at = COALESCE(decided_at, @cancelled_at)
WHERE workspace_id = @workspace_id
  AND id = @id
  AND agent_id = @agent_id
  AND task_id = @task_id
  AND invocation_id = @invocation_id
  AND transport_kind = @transport_kind
  AND server_key = @server_key
  AND tool_name = @tool_name
  AND schema_digest = @schema_digest
  AND policy_revision = @policy_revision
  AND status IN ('pending', 'approved')
  AND expires_at > @cancelled_at
RETURNING *;

-- name: CancelAgentToolApprovalRequestsBeforePolicyRevision :many
UPDATE agent_tool_approval_request
SET status = 'cancelled',
    reason_code = 'policy_replaced',
    decided_at = COALESCE(decided_at, @cancelled_at)
WHERE workspace_id = @workspace_id
  AND agent_id = @agent_id
  AND policy_revision < @active_policy_revision
  AND status IN ('pending', 'approved')
RETURNING *;

-- name: CancelAgentToolApprovalRequestsForTask :many
UPDATE agent_tool_approval_request
SET status = 'cancelled',
    reason_code = 'task_cancelled',
    decided_at = COALESCE(decided_at, @cancelled_at)
WHERE workspace_id = @workspace_id
  AND task_id = @task_id
  AND status IN ('pending', 'approved')
RETURNING *;

-- name: CancelAgentToolApprovalRequestsForAgentCleanup :many
UPDATE agent_tool_approval_request
SET status = 'cancelled',
    reason_code = 'agent_cleanup',
    decided_at = COALESCE(decided_at, @cancelled_at)
WHERE workspace_id = @workspace_id
  AND agent_id = @agent_id
  AND status IN ('pending', 'approved')
RETURNING *;

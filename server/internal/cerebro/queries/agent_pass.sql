-- Agent-pass CRUD (JEH-1327).
-- The pre-run gate path (GetActiveAgentPassForAgentIssue) is on the hot
-- path of every task claim, so it MUST use the partial index
-- idx_cerebro_agent_pass_active_agent_issue.

-- name: CreateCerebroAgentPass :one
INSERT INTO cerebro_agent_pass (
    workspace_id,
    issuer_type, issuer_id,
    agent_id, issue_id,
    parent_pass_id,
    scope,
    spend_ceiling_micros,
    expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
RETURNING
    id, workspace_id,
    issuer_type, issuer_id,
    agent_id, issue_id, parent_pass_id,
    scope,
    spend_ceiling_micros,
    status,
    issued_at, expires_at,
    revoked_by_type, revoked_by_id, revoked_at, revoke_reason,
    created_at, updated_at;

-- name: GetCerebroAgentPass :one
SELECT
    id, workspace_id,
    issuer_type, issuer_id,
    agent_id, issue_id, parent_pass_id,
    scope,
    spend_ceiling_micros,
    status,
    issued_at, expires_at,
    revoked_by_type, revoked_by_id, revoked_at, revoke_reason,
    created_at, updated_at
FROM cerebro_agent_pass
WHERE id = $1;

-- name: GetActiveAgentPassForAgentIssue :one
-- Returns the single active pass for (agent, issue) if one exists.
-- Used by the pre-run gate. The partial index makes this O(log n) per call.
SELECT
    id, workspace_id,
    issuer_type, issuer_id,
    agent_id, issue_id, parent_pass_id,
    scope,
    spend_ceiling_micros,
    status,
    issued_at, expires_at,
    revoked_by_type, revoked_by_id, revoked_at, revoke_reason,
    created_at, updated_at
FROM cerebro_agent_pass
WHERE agent_id = $1
  AND issue_id = $2
  AND status   = 'active'
ORDER BY issued_at DESC
LIMIT 1;

-- name: ListActiveAgentPassesForWorkspace :many
SELECT
    id, workspace_id,
    issuer_type, issuer_id,
    agent_id, issue_id, parent_pass_id,
    scope,
    spend_ceiling_micros,
    status,
    issued_at, expires_at,
    revoked_by_type, revoked_by_id, revoked_at, revoke_reason,
    created_at, updated_at
FROM cerebro_agent_pass
WHERE workspace_id = $1
  AND status       = 'active'
ORDER BY issued_at DESC;

-- name: MarkAgentPassStatus :one
-- Used by the gate to transition active → expired/exhausted when it
-- detects a terminal condition during a claim. Idempotent — only flips
-- from 'active' so a concurrent revoke wins.
UPDATE cerebro_agent_pass
SET status     = $2,
    updated_at = now()
WHERE id        = $1
  AND status    = 'active'
RETURNING
    id, workspace_id,
    issuer_type, issuer_id,
    agent_id, issue_id, parent_pass_id,
    scope,
    spend_ceiling_micros,
    status,
    issued_at, expires_at,
    revoked_by_type, revoked_by_id, revoked_at, revoke_reason,
    created_at, updated_at;

-- name: RevokeAgentPass :one
UPDATE cerebro_agent_pass
SET status          = 'revoked',
    revoked_by_type = $2,
    revoked_by_id   = $3,
    revoked_at      = now(),
    revoke_reason   = $4,
    updated_at      = now()
WHERE id     = $1
  AND status = 'active'
RETURNING
    id, workspace_id,
    issuer_type, issuer_id,
    agent_id, issue_id, parent_pass_id,
    scope,
    spend_ceiling_micros,
    status,
    issued_at, expires_at,
    revoked_by_type, revoked_by_id, revoked_at, revoke_reason,
    created_at, updated_at;

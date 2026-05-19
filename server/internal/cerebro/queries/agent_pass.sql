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

-- name: GetExhaustedAgentPassForAgentIssue :one
-- Returns the most recently exhausted pass for (agent, issue), if any.
-- Used by the pre-run gate as a fallback after the active-pass lookup finds
-- nothing: a pass flipped to 'exhausted' on a prior enqueue must still block
-- subsequent enqueues until the ceiling is raised or a new pass is issued.
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
  AND status   = 'exhausted'
ORDER BY updated_at DESC
LIMIT 1;

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

-- name: ListCerebroAgentPassesForWorkspace :many
-- Admin UI listing: returns passes in all lifecycle states so the operator
-- can see active, revoked, expired and exhausted side-by-side. Active rows
-- surface first; terminal-state rows trail by most recently updated.
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
ORDER BY
    CASE status WHEN 'active' THEN 0 ELSE 1 END,
    issued_at DESC,
    updated_at DESC;

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

-- name: SumIssueTreeUsageByModel :many
-- Returns per-(provider, model) token sums for all tasks whose issue is in
-- the subtree rooted at $1 (inclusive). Used by the spend-ceiling gate
-- (JEH-1327 milestone 3) to compute accumulated cost against the pass ceiling.
-- The recursive CTE traverses child issues via parent_issue_id so the sum
-- covers the entire issue tree, not just the root issue.
WITH RECURSIVE issue_tree AS (
    SELECT issue.id AS issue_id FROM issue WHERE issue.id = $1
    UNION ALL
    SELECT i.id AS issue_id
    FROM issue i
    JOIN issue_tree t ON i.parent_issue_id = t.issue_id
)
SELECT
    tu.provider,
    tu.model,
    SUM(tu.input_tokens)::bigint        AS input_tokens,
    SUM(tu.output_tokens)::bigint       AS output_tokens,
    SUM(tu.cache_read_tokens)::bigint   AS cache_read_tokens,
    SUM(tu.cache_write_tokens)::bigint  AS cache_write_tokens
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
WHERE atq.issue_id IN (SELECT t2.issue_id FROM issue_tree t2)
GROUP BY tu.provider, tu.model;

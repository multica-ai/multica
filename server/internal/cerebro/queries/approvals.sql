-- Approval inbox CRUD + audit (JEH-2131 / FIR-2131).

-- name: CreateCerebroApprovalRequest :one
INSERT INTO cerebro_approval_request (
    workspace_id,
    requester_type, requester_id,
    agent_id,
    capability, resource, reason,
    matched_grant_ids, context,
    expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
)
RETURNING
    id, workspace_id,
    requester_type, requester_id, agent_id,
    capability, resource, reason, matched_grant_ids, context,
    status,
    decided_by_id, decided_at, decision_note,
    delegated_to_type, delegated_to_id,
    expires_at, created_at, updated_at;

-- name: GetCerebroApprovalRequest :one
SELECT
    id, workspace_id,
    requester_type, requester_id, agent_id,
    capability, resource, reason, matched_grant_ids, context,
    status,
    decided_by_id, decided_at, decision_note,
    delegated_to_type, delegated_to_id,
    expires_at, created_at, updated_at
FROM cerebro_approval_request
WHERE id = $1 AND workspace_id = $2;

-- name: ListCerebroApprovalRequests :many
SELECT
    id, workspace_id,
    requester_type, requester_id, agent_id,
    capability, resource, reason, matched_grant_ids, context,
    status,
    decided_by_id, decided_at, decision_note,
    delegated_to_type, delegated_to_id,
    expires_at, created_at, updated_at,
    count(*) OVER()::int AS total
FROM cerebro_approval_request
WHERE workspace_id = $1
  -- Treat an omitted status as "any status".
  AND (NULLIF($2::text, '') IS NULL OR status = $2)
ORDER BY
    -- Pending asks float to the top; otherwise newest first.
    (status = 'pending') DESC,
    created_at DESC
LIMIT $3 OFFSET $4;

-- name: CountPendingCerebroApprovals :one
SELECT count(*)::int AS pending
FROM cerebro_approval_request
WHERE workspace_id = $1 AND status = 'pending';

-- name: DecideCerebroApprovalRequest :one
-- Race-safe terminal decision: only a still-pending row transitions. Two
-- concurrent approvers contend on the row lock; the loser matches zero rows
-- and the query returns pgx.ErrNoRows, which the service maps to
-- ErrAlreadyDecided. $3 is the new status ('approved' | 'rejected').
UPDATE cerebro_approval_request
SET
    status        = $3,
    decided_by_id = $4,
    decided_at    = now(),
    decision_note = $5,
    updated_at    = now()
WHERE id = $1 AND workspace_id = $2 AND status = 'pending'
RETURNING
    id, workspace_id,
    requester_type, requester_id, agent_id,
    capability, resource, reason, matched_grant_ids, context,
    status,
    decided_by_id, decided_at, decision_note,
    delegated_to_type, delegated_to_id,
    expires_at, created_at, updated_at;

-- name: DelegateCerebroApprovalRequest :one
-- Delegation keeps the ask actionable: it stays effectively open but is
-- reassigned to another approver. Only a still-pending row delegates.
UPDATE cerebro_approval_request
SET
    status            = 'delegated',
    delegated_to_type = $3,
    delegated_to_id   = $4,
    decided_by_id     = $5,
    decided_at        = now(),
    decision_note     = $6,
    updated_at        = now()
WHERE id = $1 AND workspace_id = $2 AND status = 'pending'
RETURNING
    id, workspace_id,
    requester_type, requester_id, agent_id,
    capability, resource, reason, matched_grant_ids, context,
    status,
    decided_by_id, decided_at, decision_note,
    delegated_to_type, delegated_to_id,
    expires_at, created_at, updated_at;

-- name: ExpireDueCerebroApprovalRequests :many
-- Sweep pending asks past their deadline to 'expired'. Returns the affected
-- ids so the caller can write one audit row per expiry.
UPDATE cerebro_approval_request
SET status = 'expired', updated_at = now()
WHERE status = 'pending'
  AND expires_at IS NOT NULL
  AND expires_at <= now()
RETURNING id, workspace_id;

-- name: ListCerebroApprovalAudit :many
SELECT
    a.id, a.workspace_id, a.approval_id, a.action,
    a.actor_type, a.actor_id,
    COALESCE(u.name, ag.name, '') AS actor_name,
    a.surface, a.note, a.created_at,
    count(*) OVER()::int AS total
FROM cerebro_approval_audit a
LEFT JOIN "user" u
  ON a.actor_type = 'member' AND u.id = a.actor_id
LEFT JOIN agent ag
  ON a.actor_type = 'agent' AND ag.id = a.actor_id
WHERE a.workspace_id = $1
  AND ($2::uuid IS NULL OR a.approval_id = $2)
ORDER BY a.created_at DESC
LIMIT $3 OFFSET $4;

-- name: InsertCerebroApprovalAudit :exec
INSERT INTO cerebro_approval_audit (
    workspace_id, approval_id, action,
    actor_type, actor_id,
    surface, note
) VALUES ($1, $2, $3, $4, $5, $6, $7);

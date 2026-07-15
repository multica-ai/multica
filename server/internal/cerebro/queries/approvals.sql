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
    expires_at, created_at, updated_at, single_use, consumed_at;

-- name: GetCerebroApprovalRequest :one
SELECT
    id, workspace_id,
    requester_type, requester_id, agent_id,
    capability, resource, reason, matched_grant_ids, context,
    status,
    decided_by_id, decided_at, decision_note,
    delegated_to_type, delegated_to_id,
    expires_at, created_at, updated_at, single_use, consumed_at
FROM cerebro_approval_request
WHERE id = $1 AND workspace_id = $2;

-- name: FindReusableCerebroApprovalRequest :one
-- Idempotency for poll-based callers (repo checkout, FIR-2586). A daemon repo
-- checkout that resolves to "Ask" cannot block the short-timeout HTTP request,
-- so it creates an ask, long-polls for a few minutes, then gives up and the
-- agent retries. Without dedup each retry would spawn a fresh inbox request,
-- and an ask approved after the poll budget would never be honoured. This finds
-- the most recent still-actionable approval for the same (agent, capability,
-- resource): a 'pending' row lets the retry rejoin the SAME ask; an 'approved'
-- row that has not expired lets the retry through. Approved wins over pending so
-- a granted checkout proceeds as soon as the human says yes. Negative-terminal
-- rows (rejected/expired/cancelled/delegated) are ignored so a fresh ask is
-- raised.
SELECT
    id, workspace_id,
    requester_type, requester_id, agent_id,
    capability, resource, reason, matched_grant_ids, context,
    status,
    decided_by_id, decided_at, decision_note,
    delegated_to_type, delegated_to_id,
    expires_at, created_at, updated_at, single_use, consumed_at
FROM cerebro_approval_request
WHERE workspace_id = $1
  AND agent_id IS NOT DISTINCT FROM $2
  AND capability = $3
  AND resource = $4
  AND (status = 'pending' OR (status = 'approved' AND single_use = FALSE))
  AND (expires_at IS NULL OR expires_at > now())
ORDER BY
    (status = 'approved') DESC,
    created_at DESC
LIMIT 1;

-- name: ListCerebroApprovalRequests :many
SELECT
    id, workspace_id,
    requester_type, requester_id, agent_id,
    capability, resource, reason, matched_grant_ids, context,
    status,
    decided_by_id, decided_at, decision_note,
    delegated_to_type, delegated_to_id,
    expires_at, created_at, updated_at, single_use, consumed_at,
    count(*) OVER()::int AS total
FROM cerebro_approval_request
WHERE workspace_id = sqlc.arg(workspace_id)
  -- Treat an omitted status as "any status".
  AND (NULLIF(sqlc.arg(status)::text, '') IS NULL OR status = sqlc.arg(status))
  AND (NULLIF(sqlc.arg(task_id)::text, '') IS NULL OR context->>'task_id' = sqlc.arg(task_id))
  AND (NULLIF(sqlc.arg(issue_id)::text, '') IS NULL OR context->>'issue_id' = sqlc.arg(issue_id))
  AND (NULLIF(sqlc.arg(chat_session_id)::text, '') IS NULL OR context->>'chat_session_id' = sqlc.arg(chat_session_id))
  AND (NULLIF(sqlc.arg(trigger_comment_id)::text, '') IS NULL OR context->>'trigger_comment_id' = sqlc.arg(trigger_comment_id))
  AND (NULLIF(sqlc.arg(surface)::text, '') IS NULL OR context->>'surface' = sqlc.arg(surface))
ORDER BY
    -- Pending asks float to the top; otherwise newest first.
    (status = 'pending') DESC,
    created_at DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountPendingCerebroApprovals :one
SELECT count(*)::int AS pending
FROM cerebro_approval_request
WHERE workspace_id = $1 AND status = 'pending';

-- name: DecideCerebroApprovalRequest :one
-- Race-safe terminal decision: only a still-pending row transitions. Two
-- concurrent approvers contend on the row lock; the loser matches zero rows
-- and the query returns pgx.ErrNoRows, which the service maps to
-- ErrAlreadyDecided. $3 is the new status ('approved' | 'rejected'). $6 sets
-- expires_at so an approver can time-box a grant: NULL = never, now() = a
-- one-shot or period-grant expiry. $7 is the authoritative reuse mode:
-- single-use approvals must be consumed exactly once; period grants are the
-- only approved rows FindReusable may return (TECH-3498, FIR-3324).
UPDATE cerebro_approval_request
SET
    status        = $3,
    decided_by_id = $4,
    decided_at    = now(),
    decision_note = $5,
    expires_at    = $6,
    single_use    = $7,
    consumed_at   = NULL,
    updated_at    = now()
WHERE id = $1 AND workspace_id = $2 AND status = 'pending'
RETURNING
    id, workspace_id,
    requester_type, requester_id, agent_id,
    capability, resource, reason, matched_grant_ids, context,
    status,
    decided_by_id, decided_at, decision_note,
    delegated_to_type, delegated_to_id,
    expires_at, created_at, updated_at, single_use, consumed_at;

-- name: ConsumeApprovedCerebroApprovalRequest :one
-- Atomically consume an exact approved request. The id is only a reference:
-- workspace, agent, capability and resource must all match the original call.
UPDATE cerebro_approval_request
SET
    consumed_at = CASE WHEN single_use THEN now() ELSE consumed_at END,
    updated_at = now()
WHERE id = $1
  AND workspace_id = $2
  AND agent_id IS NOT DISTINCT FROM $3
  AND capability = $4
  AND resource = $5
  AND status = 'approved'
  AND (expires_at IS NULL OR expires_at > now())
  AND (single_use = FALSE OR consumed_at IS NULL)
RETURNING
    id, workspace_id,
    requester_type, requester_id, agent_id,
    capability, resource, reason, matched_grant_ids, context,
    status,
    decided_by_id, decided_at, decision_note,
    delegated_to_type, delegated_to_id,
    expires_at, created_at, updated_at, single_use, consumed_at;

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
    expires_at, created_at, updated_at, single_use, consumed_at;

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

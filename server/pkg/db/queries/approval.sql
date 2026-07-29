-- =====================
-- Approval Requests (WS-721 sensitive-operation approval flow)
-- =====================

-- name: CreateApprovalRequest :one
INSERT INTO approval_request (
    workspace_id, operation, target_type, target_id, reason,
    initiated_by_type, initiated_by_id, payload, expires_at
) VALUES (
    $1, $2, $3, sqlc.narg('target_id'), $4,
    $5, $6, $7, sqlc.narg('expires_at')
) RETURNING *;

-- name: GetApprovalRequest :one
SELECT * FROM approval_request
WHERE id = $1;

-- name: GetApprovalRequestInWorkspace :one
SELECT * FROM approval_request
WHERE id = $1 AND workspace_id = $2;

-- name: ListApprovalRequests :many
-- Workspace-scoped listing. status is an optional filter (NULL = all). The
-- limit caps the page; the handler requests one extra row to detect has_more.
SELECT * FROM approval_request
WHERE workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY created_at DESC
LIMIT $2;

-- name: ListPendingApprovalRequests :many
-- "Awaiting decision" dashboard: pending requests for a workspace, newest first.
SELECT * FROM approval_request
WHERE workspace_id = $1 AND status = 'pending'
ORDER BY created_at DESC
LIMIT $2;

-- name: DecideApprovalRequest :one
-- Atomically move a pending request to a terminal decision (approved/rejected).
-- Returns no rows if the request is no longer pending, which the handler maps
-- to a 409 conflict. Encoding pending->approved/rejected as a single
-- conditional UPDATE means two concurrent decides cannot both succeed.
UPDATE approval_request
SET status = $3,
    decided_by_type = $4,
    decided_by_id = $5,
    decided_at = now(),
    decision_comment = $6,
    updated_at = now()
WHERE id = $1
  AND workspace_id = $2
  AND status = 'pending'
RETURNING *;

-- name: CancelApprovalRequest :one
-- Only the initiator may cancel, and only while still pending.
UPDATE approval_request
SET status = 'cancelled', updated_at = now()
WHERE id = $1
  AND workspace_id = $2
  AND status = 'pending'
  AND initiated_by_type = $3
  AND initiated_by_id = $4
RETURNING *;

-- name: MarkApprovalRequestExecuted :exec
-- Record that the approved action ran. execution_error is NULL on success.
UPDATE approval_request
SET executed_at = now(),
    execution_error = sqlc.narg('execution_error'),
    updated_at = now()
WHERE id = $1 AND status = 'approved';

-- name: ExpirePendingApprovalRequests :many
-- Sweep: flip pending requests past their expires_at to expired. Returns the
-- rows so the caller can append an approval_event per request.
UPDATE approval_request
SET status = 'expired', updated_at = now()
WHERE status = 'pending'
  AND expires_at IS NOT NULL
  AND expires_at < now()
RETURNING *;

-- =====================
-- Approval Events (audit trail / approval history)
-- =====================

-- name: CreateApprovalEvent :one
INSERT INTO approval_event (
    approval_request_id, workspace_id, event_type,
    actor_type, actor_id, comment, details
) VALUES (
    $1, $2, $3,
    sqlc.narg('actor_type'), sqlc.narg('actor_id'), $4, $5
) RETURNING *;

-- name: ListApprovalEvents :many
-- Full history for one request, oldest first.
SELECT * FROM approval_event
WHERE approval_request_id = $1
ORDER BY created_at ASC, id ASC;

-- name: ListApprovalEventsForWorkspace :many
-- Workspace-wide audit timeline, newest first, capped by the handler.
SELECT * FROM approval_event
WHERE workspace_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2;

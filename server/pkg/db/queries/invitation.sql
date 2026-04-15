-- name: CreateInvitation :one
INSERT INTO workspace_invitation (workspace_id, inviter_id, invitee_email, invitee_user_id, role)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetInvitation :one
SELECT * FROM workspace_invitation
WHERE id = $1;

-- name: ListPendingInvitationsByWorkspace :many
SELECT wi.*,
       u.name  AS inviter_name,
       u.email AS inviter_email
FROM workspace_invitation wi
JOIN "user" u ON u.id = wi.inviter_id
WHERE wi.workspace_id = $1 AND wi.status = 'pending' AND wi.expires_at > now()
ORDER BY wi.created_at DESC;

-- name: ListPendingInvitationsForUser :many
SELECT wi.*,
       w.name AS workspace_name,
       u.name AS inviter_name,
       u.email AS inviter_email
FROM workspace_invitation wi
JOIN workspace w ON w.id = wi.workspace_id
JOIN "user" u ON u.id = wi.inviter_id
WHERE wi.status = 'pending'
  AND (wi.invitee_user_id = $1 OR wi.invitee_email = $2)
  AND wi.expires_at > now()
ORDER BY wi.created_at DESC;

-- name: AcceptInvitation :one
UPDATE workspace_invitation
SET status = 'accepted', updated_at = now()
WHERE id = $1 AND status = 'pending'
RETURNING *;

-- name: DeclineInvitation :one
UPDATE workspace_invitation
SET status = 'declined', updated_at = now()
WHERE id = $1 AND status = 'pending'
RETURNING *;

-- name: RevokeInvitation :exec
DELETE FROM workspace_invitation
WHERE id = $1 AND status = 'pending';

-- name: GetPendingInvitationByEmail :one
SELECT * FROM workspace_invitation
WHERE workspace_id = $1 AND invitee_email = $2 AND status = 'pending';

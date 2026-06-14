-- name: CreateInvitation :one
INSERT INTO workspace_invitation (workspace_id, inviter_id, invitee_email, invitee_user_id, role, invitee_name)
VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))
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
WHERE workspace_id = $1 AND invitee_email = $2 AND status = 'pending' AND expires_at > now();

-- name: ExpireStalePendingInvitations :exec
-- Mark any past-due pending invitations for (workspace_id, invitee_email) as expired,
-- so the next CreateInvitation does not collide with the partial unique index
-- idx_invitation_unique_pending (which is WHERE status = 'pending' and cannot
-- itself reference now() in its predicate).
UPDATE workspace_invitation
SET status = 'expired', updated_at = now()
WHERE workspace_id = $1
  AND invitee_email = $2
  AND status = 'pending'
  AND expires_at <= now();

-- name: GetLatestPendingInvitationNameByEmail :one
-- Returns the invitee_name from the most recent non-expired pending invitation
-- for this email address that has a non-empty invitee_name. Used at registration
-- time to set user.name from the invitation rather than the email prefix or OAuth name.
-- Uses the same expires_at > now() guard as every other live-invitation query in this
-- file (expiry is lazy — rows stay pending until CreateInvitation flips them).
-- The secondary id DESC sort makes the result deterministic when two invitations share
-- the same created_at timestamp.
SELECT invitee_name FROM workspace_invitation
WHERE invitee_email = $1
  AND status = 'pending'
  AND expires_at > now()
  AND invitee_name IS NOT NULL
  AND invitee_name <> ''
ORDER BY created_at DESC, id DESC
LIMIT 1;

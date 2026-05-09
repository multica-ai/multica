-- name: CreateInvitation :one
INSERT INTO workspace_invitation (workspace_id, inviter_id, invitee_email, invitee_user_id, role, max_uses)
VALUES ($1, $2, $3, $4, $5, $6)
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

-- name: RedeemShareableInvitation :one
-- Atomic redeem step for shareable links: bump use_count; if max_uses is
-- reached the row transitions to 'accepted' so it can no longer be used.
-- Matches only pending, non-expired links that have remaining capacity.
UPDATE workspace_invitation
SET use_count  = use_count + 1,
    status     = CASE
                   WHEN max_uses IS NOT NULL AND use_count + 1 >= max_uses THEN 'accepted'
                   ELSE status
                 END,
    updated_at = now()
WHERE id = $1
  AND status = 'pending'
  AND invitee_email IS NULL
  AND expires_at > now()
  AND (max_uses IS NULL OR use_count < max_uses)
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

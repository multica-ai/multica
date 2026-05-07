-- name: CreateInvitation :one
INSERT INTO workspace_invitation (workspace_id, inviter_id, invitee_email, invitee_user_id, role)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: CreateInviteLink :one
INSERT INTO workspace_invitation (
    workspace_id,
    inviter_id,
    invite_type,
    token_hash,
    role,
    expires_at,
    max_uses,
    created_by_ip,
    created_by_user_agent
)
VALUES ($1, $2, 'link', $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetInvitation :one
SELECT * FROM workspace_invitation
WHERE id = $1;

-- name: GetInviteLinkByTokenHash :one
SELECT wi.*,
       w.name AS workspace_name,
       u.name AS inviter_name,
       u.email AS inviter_email
FROM workspace_invitation wi
JOIN workspace w ON w.id = wi.workspace_id
JOIN "user" u ON u.id = wi.inviter_id
WHERE wi.invite_type = 'link' AND wi.token_hash = $1;

-- name: GetInviteLinkByID :one
SELECT wi.*,
       w.name AS workspace_name,
       u.name AS inviter_name,
       u.email AS inviter_email
FROM workspace_invitation wi
JOIN workspace w ON w.id = wi.workspace_id
JOIN "user" u ON u.id = wi.inviter_id
WHERE wi.invite_type = 'link' AND wi.id = $1;

-- name: GetInviteLinkByTokenHashForUpdate :one
SELECT * FROM workspace_invitation
WHERE invite_type = 'link' AND token_hash = $1
FOR UPDATE;

-- name: GetInviteLinkByIDForUpdate :one
SELECT * FROM workspace_invitation
WHERE invite_type = 'link' AND id = $1
FOR UPDATE;

-- name: ListPendingInvitationsByWorkspace :many
SELECT wi.*,
       u.name  AS inviter_name,
       u.email AS inviter_email
FROM workspace_invitation wi
JOIN "user" u ON u.id = wi.inviter_id
WHERE wi.workspace_id = $1 AND wi.invite_type = 'email' AND wi.status = 'pending' AND wi.expires_at > now()
ORDER BY wi.created_at DESC;

-- name: ListInviteLinksByWorkspace :many
SELECT wi.*,
       u.name AS inviter_name,
       u.email AS inviter_email
FROM workspace_invitation wi
JOIN "user" u ON u.id = wi.inviter_id
WHERE wi.workspace_id = $1 AND wi.invite_type = 'link'
ORDER BY wi.created_at DESC;

-- name: ListPendingInvitationsForUser :many
SELECT wi.*,
       w.name AS workspace_name,
       u.name AS inviter_name,
       u.email AS inviter_email
FROM workspace_invitation wi
JOIN workspace w ON w.id = wi.workspace_id
JOIN "user" u ON u.id = wi.inviter_id
WHERE wi.invite_type = 'email' AND wi.status = 'pending'
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

-- name: RevokeInviteLink :one
UPDATE workspace_invitation
SET status = 'expired', revoked_at = now(), updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND invite_type = 'link' AND revoked_at IS NULL
RETURNING *;

-- name: DeleteInviteLink :exec
DELETE FROM workspace_invitation
WHERE id = $1 AND workspace_id = $2 AND invite_type = 'link';

-- name: ConsumeInviteLink :one
UPDATE workspace_invitation
SET used_count = used_count + 1,
    last_used_at = now(),
    updated_at = now(),
    status = CASE WHEN used_count + 1 >= max_uses THEN 'accepted' ELSE status END
WHERE id = $1
  AND invite_type = 'link'
  AND revoked_at IS NULL
  AND status = 'pending'
  AND expires_at > now()
  AND used_count < max_uses
RETURNING *;

-- name: GetPendingInvitationByEmail :one
SELECT * FROM workspace_invitation
WHERE workspace_id = $1 AND invite_type = 'email' AND invitee_email = $2 AND status = 'pending';

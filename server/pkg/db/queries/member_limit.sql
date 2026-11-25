-- Member limit (entitlement gate "members").

-- name: LockWorkspaceMemberAdmission :exec
-- Serializes member-limit admissions within one workspace so the count and
-- the write it guards see the same members and invitations.
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg('workspace_id')::uuid::text || ':member_admission', 0));

-- name: CountWorkspaceMemberSlots :one
-- Members and live pending invitations that take a member slot. Guests take
-- none.
SELECT
    (SELECT count(*)
       FROM member m
      WHERE m.workspace_id = sqlc.arg('workspace_id')
        AND m.role <> 'guest')::bigint AS members,
    (SELECT count(*)
       FROM workspace_invitation wi
      WHERE wi.workspace_id = sqlc.arg('workspace_id')
        AND wi.status = 'pending'
        AND wi.expires_at > now()
        AND wi.role <> 'guest')::bigint AS pending_invitations;

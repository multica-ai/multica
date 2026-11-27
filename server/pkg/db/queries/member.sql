-- name: ListMembers :many
SELECT * FROM member
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: ListDepartedMemberAssetHolders :many
-- (workspace, user) pairs where a user who is no longer a member still owns a
-- live agent, an active autopilot or an enabled wakeup: members removed
-- before the removal flow retired these assets. A guest is still a member.
SELECT DISTINCT refs.workspace_id::uuid AS workspace_id, refs.user_id::uuid AS user_id
FROM (
    SELECT workspace_id, owner_id AS user_id FROM agent
    WHERE archived_at IS NULL AND kind = 'user' AND owner_id IS NOT NULL
    UNION
    SELECT workspace_id, created_by_id FROM autopilot
    WHERE status = 'active' AND created_by_type = 'member' AND created_by_id IS NOT NULL
    UNION
    SELECT workspace_id, created_by FROM issue_wakeup
    WHERE disabled_at IS NULL AND system_rule IS NULL AND created_by IS NOT NULL
    UNION
    SELECT workspace_id, requester_id FROM chat_wakeup
    WHERE status = 'active'
) refs
WHERE NOT EXISTS (
    SELECT 1 FROM member m
    WHERE m.workspace_id = refs.workspace_id AND m.user_id = refs.user_id
)
ORDER BY 1, 2;

-- name: ListWorkspaceManagerUserIDs :many
SELECT user_id FROM member
WHERE workspace_id = $1 AND role IN ('owner', 'admin')
ORDER BY created_at ASC;

-- name: GetMember :one
SELECT * FROM member
WHERE id = $1;

-- name: GetMemberByUserAndWorkspace :one
-- A role this build does not know is no membership: every access check that
-- starts from this lookup refuses it. A later release can then add a role
-- and still be rolled back to this one without the old build reading the new
-- role as a full member, the way builds before guests read 'guest'.
SELECT * FROM member
WHERE user_id = $1 AND workspace_id = $2
  AND role IN ('owner', 'admin', 'member', 'guest');

-- name: GetRealtimeMemberships :many
SELECT m.user_id, m.workspace_id
FROM member m
JOIN (
    SELECT unnest(sqlc.arg(user_ids)::uuid[]) AS user_id,
           unnest(sqlc.arg(workspace_ids)::uuid[]) AS workspace_id
) AS requested
    ON m.user_id = requested.user_id AND m.workspace_id = requested.workspace_id
-- Guests (MUL-9420) get no realtime stream: workspace fanout is not narrowed
-- to their projects, so a socket that becomes a guest's is closed. Neither
-- does a role this build does not know (see GetMemberByUserAndWorkspace).
WHERE m.role IN ('owner', 'admin', 'member');

-- name: CreateMember :one
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, $3)
RETURNING *;

-- name: UpdateMemberRole :one
UPDATE member SET role = $2
WHERE id = $1
RETURNING *;

-- name: GetMemberForUpdate :one
-- Locks a member row for a role change, so concurrent changes to the same
-- member apply one after the other and each re-checks the role it starts from.
SELECT * FROM member WHERE id = $1 FOR UPDATE;

-- name: LockWorkspaceOwners :many
-- Locks a workspace's owner rows in id order, so changes that could take away
-- its last owner apply one after the other and each counts the owners the
-- previous one left.
SELECT id FROM member
WHERE workspace_id = $1 AND role = 'owner'
ORDER BY id
FOR UPDATE;

-- name: DeleteMember :exec
DELETE FROM member WHERE id = $1;

-- name: ListMembersWithUser :many
SELECT m.id, m.workspace_id, m.user_id, m.role, m.created_at,
       u.name as user_name, u.email as user_email, u.avatar_url as user_avatar_url
FROM member m
JOIN "user" u ON u.id = m.user_id
WHERE m.workspace_id = $1
ORDER BY m.created_at ASC;

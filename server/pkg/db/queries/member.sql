-- name: ListMembers :many
SELECT * FROM member
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: GetMember :one
SELECT * FROM member
WHERE id = $1;

-- name: GetMemberInWorkspace :one
-- Workspace-scoped member existence check by member row id. Used to fail-closed
-- validate a send_inbox action target belongs to the hook's workspace
-- (MUL-4332 PR2 review point 2).
SELECT * FROM member
WHERE id = $1 AND workspace_id = $2;

-- name: GetMemberByUserAndWorkspace :one
SELECT * FROM member
WHERE user_id = $1 AND workspace_id = $2;

-- name: GetMemberByUserAndWorkspaceForShare :one
-- Locking membership read for authorization inside a write transaction. FOR SHARE
-- takes a shared row lock that blocks a concurrent role UPDATE (demotion) or member
-- DELETE (removal) from committing until this transaction ends, so a hook write can
-- never commit under a membership/role that was revoked mid-transaction — a plain
-- read under READ COMMITTED would miss that (MUL-4332 PR2 review round 5). Multiple
-- concurrent hook writes may still share-lock the same member without blocking each
-- other. Use ONLY inside the hook write transaction; the plain variant remains for
-- non-transactional reads.
SELECT * FROM member
WHERE user_id = $1 AND workspace_id = $2
FOR SHARE;

-- name: CreateMember :one
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, $3)
RETURNING *;

-- name: UpdateMemberRole :one
UPDATE member SET role = $2
WHERE id = $1
RETURNING *;

-- name: DeleteMember :exec
DELETE FROM member WHERE id = $1;

-- name: ListMembersWithUser :many
SELECT m.id, m.workspace_id, m.user_id, m.role, m.created_at,
       u.name as user_name, u.email as user_email, u.avatar_url as user_avatar_url
FROM member m
JOIN "user" u ON u.id = m.user_id
WHERE m.workspace_id = $1
ORDER BY m.created_at ASC;

-- name: ListWorkspaceMembersByRoles :many
-- Members holding any of the given roles, for notifications that must reach the
-- people able to act on them (e.g. an automation paused because its authorization
-- principal left). Role filtering elsewhere is a per-caller authorization check;
-- this is the fan-out selector, which did not previously exist.
SELECT * FROM member
WHERE workspace_id = $1 AND role = ANY(@roles::text[])
ORDER BY created_at ASC;

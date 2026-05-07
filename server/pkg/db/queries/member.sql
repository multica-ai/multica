-- CEREBRO-PATCH(sqlc-member): cerebro modification of upstream file
-- name: ListMembers :many
SELECT * FROM member
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: GetMember :one
SELECT * FROM member
WHERE id = $1;

-- name: GetMemberByUserAndWorkspace :one
SELECT * FROM member
WHERE user_id = $1 AND workspace_id = $2;

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
       m.budget_enforcement_enabled,
       u.name as user_name, u.email as user_email, u.avatar_url as user_avatar_url
FROM member m
JOIN "user" u ON u.id = m.user_id
WHERE m.workspace_id = $1
ORDER BY m.created_at ASC;

-- name: GetMemberWithUser :one
SELECT m.id, m.workspace_id, m.user_id, m.role, m.created_at,
       m.budget_enforcement_enabled,
       u.name as user_name, u.email as user_email, u.avatar_url as user_avatar_url
FROM member m
JOIN "user" u ON u.id = m.user_id
WHERE m.id = $1;

-- name: UpdateMemberBudgetEnforcement :one
UPDATE member SET budget_enforcement_enabled = $2
WHERE id = $1
RETURNING *;

-- name: ListWorkspaceAdminUserIDs :many
-- Returns the user_ids of every owner/admin in the workspace. Used by the
-- WS audience filter to keep workspace governance always-on for restricted
-- and private events.
SELECT user_id FROM member
WHERE workspace_id = $1 AND role IN ('owner', 'admin');
